// Package godotenv is a go port of the ruby dotenv library (https://github.com/bkeepers/dotenv)
//
// Examples/readme can be found on the github page at https://github.com/joho/godotenv
//
// The TL;DR is that you make a .env file that looks something like
//
// 		SOME_ENV_VAR=somevalue
//
// and then in your go code you can call
//
// 		godotenv.Load()
//
// and all the env vars declared in .env will be available through os.Getenv("SOME_ENV_VAR")
package godotenv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const (
	doubleQuoteSpecialChars = "\\\n\r\"!$`"
)

var FileName = ".env"

var KVFormat = func(k, v string, max byte) string {
	if d, err := strconv.Atoi(v); err == nil {
		return fmt.Sprintf(fmt.Sprintf("%%-%ds = %%-%dd\n", max, max), k, d)
	} else {
		return fmt.Sprintf(fmt.Sprintf("%%-%ds = %%-%ds\n", max, max), k, DoubleQuoteEscape(v))
	}
}

// Load will read your env file(s) and load them into ENV for this process.
//
// Call this function as close as possible to the start of your program (ideally in main)
//
// If you call Load without any args it will default to loading .env in the current path
//
// You can otherwise tell it which files to load (there can be more than one) like
//
//		godotenv.Load("fileone", "filetwo")
//
// It's important to note that it WILL NOT OVERRIDE an env variable that already exists - consider the .env file to set dev vars or sensible defaults
func Load(filenames ...string) (err error) {
	filenames = filenamesOrDefault(filenames)

	for _, filename := range filenames {
		err = loadFile(filename, false)
		if err != nil {
			return // return early on a spazout
		}
	}
	return
}

// Overload will read your env file(s) and load them into ENV for this process.
//
// Call this function as close as possible to the start of your program (ideally in main)
//
// If you call Overload without any args it will default to loading .env in the current path
//
// You can otherwise tell it which files to load (there can be more than one) like
//
//		godotenv.Overload("fileone", "filetwo")
//
// It's important to note this WILL OVERRIDE an env variable that already exists - consider the .env file to forcefilly set all vars.
func Overload(filenames ...string) (err error) {
	filenames = filenamesOrDefault(filenames)

	for _, filename := range filenames {
		err = loadFile(filename, true)
		if err != nil {
			return // return early on a spazout
		}
	}
	return
}

// Read all env (with same file loading semantics as Load) but return values as
// a map rather than automatically writing values into env
func Read(filenames ...string) (ev [][2]string, err error) {
	filenames = filenamesOrDefault(filenames)
	for _, filename := range filenames {
		ev, err = readFile(filename)
		if err != nil {
			return
		}
	}
	return
}

func Range(ev [][2]string, format func(k, v string, max byte) string) (str string) {
	max := 0
	for _, v := range ev {
		if max < len(v[0]) {
			max = len(v[0])
		}
	}
	for _, v := range ev {
		str += format(v[0], v[1], byte(max))
	}
	return
}

// Show read files and return format string
func Show(filenames ...string) (str string, err error) {
	filenames = filenamesOrDefault(filenames)
	for _, filename := range filenames {
		ev, individualErr := readFile(filename)
		if individualErr != nil {
			err = individualErr
			return // return early on a spazout
		}
		str = Range(ev, KVFormat)
	}
	return
}

// Parse reads an env file from io.Reader, returning a map of keys and values.
func Parse(r io.Reader) (m [][2]string, err error) {
	envMap := make(map[string]string)

	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err = scanner.Err(); err != nil {
		return
	}

	for _, fullLine := range lines {
		if !isIgnoredLine(fullLine) {
			var key, value string
			key, value, err = parseLine(fullLine, envMap)

			if err != nil {
				return
			}
			envMap[key] = value
			m = append(m, [2]string{key, value})
		}
	}
	return
}

//Unmarshal reads an env file from a string, returning a map of keys and values.
func Unmarshal(str string) (envMap [][2]string, err error) {
	return Parse(strings.NewReader(str))
}

// Exec loads env vars from the specified filenames (empty map falls back to default)
// then executes the cmd specified.
//
// Simply hooks up os.Stdin/err/out to the command and calls Run()
//
// If you want more fine-grained control over your command it's recommended
// that you use `Load()` or `Read()` and the `os/exec` package yourself.
func Exec(filenames []string, cmd string, cmdArgs []string) error {
	err := Load(filenames...)
	if err != nil {
		return err
	}

	command := exec.Command(cmd, cmdArgs...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// Write serializes the given environment and writes it to a file
func Write(ev [][2]string, filename string) error {
	return write(Marshal(ev), filename)
}

// inner write
func write(content, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)
	_, err = file.WriteString(content + "\n")
	if err != nil {
		return err
	}
	return file.Sync()
}

// Marshal outputs the given environment as a dotenv-formatted environment file.
// Each line is in the format: KEY="VALUE" where VALUE is backslash-escaped.
func Marshal(ev [][2]string) string {
	return Range(ev, KVFormat)
}

func filenamesOrDefault(filenames []string) []string {
	if len(filenames) == 0 {
		return []string{FileName}
	}
	return filenames
}

func loadFile(filename string, overload bool) error {
	envMap, err := readFile(filename)
	if err != nil {
		return err
	}

	currentEnv := map[string]bool{}
	rawEnv := os.Environ()
	for _, rawEnvLine := range rawEnv {
		key := strings.Split(rawEnvLine, "=")[0]
		currentEnv[key] = true
	}

	for _, value := range envMap {
		if !currentEnv[value[0]] || overload {
			err := os.Setenv(value[0], value[1])
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func readFile(filename string) (envMap [][2]string, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	return Parse(file)
}

var exportRegex = regexp.MustCompile(`^\s*(?:export\s+)?(.*?)\s*$`)

func parseLine(line string, envMap map[string]string) (key string, value string, err error) {
	if len(line) == 0 {
		err = errors.New("zero length string")
		return
	}

	// ditch the comments (but keep quoted hashes)
	if strings.Contains(line, "#") {
		segmentsBetweenHashes := strings.Split(line, "#")
		quotesAreOpen := false
		var segmentsToKeep []string
		for _, segment := range segmentsBetweenHashes {
			if strings.Count(segment, "\"") == 1 || strings.Count(segment, "'") == 1 {
				if quotesAreOpen {
					quotesAreOpen = false
					segmentsToKeep = append(segmentsToKeep, segment)
				} else {
					quotesAreOpen = true
				}
			}

			if len(segmentsToKeep) == 0 || quotesAreOpen {
				segmentsToKeep = append(segmentsToKeep, segment)
			}
		}

		line = strings.Join(segmentsToKeep, "#")
	}

	firstEquals := strings.Index(line, "=")
	firstColon := strings.Index(line, ":")
	splitString := strings.SplitN(line, "=", 2)
	if firstColon != -1 && (firstColon < firstEquals || firstEquals == -1) {
		//this is a yaml-style line
		splitString = strings.SplitN(line, ":", 2)
	}

	if len(splitString) != 2 {
		err = errors.New("can't separate key from value")
		return
	}

	// Parse the key
	key = splitString[0]
	if strings.HasPrefix(key, "export") {
		key = strings.TrimPrefix(key, "export")
	}
	key = strings.TrimSpace(key)

	key = exportRegex.ReplaceAllString(splitString[0], "$1")

	// Parse the value
	value = parseValue(splitString[1], envMap)
	return
}

var (
	singleQuotesRegex  = regexp.MustCompile(`\A'(.*)'\z`)
	doubleQuotesRegex  = regexp.MustCompile(`\A"(.*)"\z`)
	escapeRegex        = regexp.MustCompile(`\\.`)
	unescapeCharsRegex = regexp.MustCompile(`\\([^$])`)
)

func parseValue(value string, envMap map[string]string) string {

	// trim
	value = strings.Trim(value, " ")

	// check if we've got quoted values or possible escapes
	if len(value) > 1 {
		singleQuotes := singleQuotesRegex.FindStringSubmatch(value)

		doubleQuotes := doubleQuotesRegex.FindStringSubmatch(value)

		if singleQuotes != nil || doubleQuotes != nil {
			// pull the quotes off the edges
			value = value[1 : len(value)-1]
		}

		if doubleQuotes != nil {
			// expand newlines
			value = escapeRegex.ReplaceAllStringFunc(value, func(match string) string {
				c := strings.TrimPrefix(match, `\`)
				switch c {
				case "n":
					return "\n"
				case "r":
					return "\r"
				default:
					return match
				}
			})
			// unescape characters
			value = unescapeCharsRegex.ReplaceAllString(value, "$1")
		}

		if singleQuotes == nil {
			value = expandVariables(value, envMap)
		}
	}

	return value
}

var expandVarRegex = regexp.MustCompile(`(\\)?(\$)(\()?{?([A-Z0-9_]+)?}?`)

func expandVariables(v string, m map[string]string) string {
	return expandVarRegex.ReplaceAllStringFunc(v, func(s string) string {
		sub := expandVarRegex.FindStringSubmatch(s)

		if sub == nil {
			return s
		}
		if sub[1] == "\\" || sub[2] == "(" {
			return sub[0][1:]
		} else if sub[4] != "" {
			return m[sub[4]]
		}
		return s
	})
}

func isIgnoredLine(line string) bool {
	trimmedLine := strings.TrimSpace(line)
	return len(trimmedLine) == 0 || strings.HasPrefix(trimmedLine, "#")
}

func DoubleQuoteEscape(line string) string {
	for _, c := range doubleQuoteSpecialChars {
		toReplace := "\\" + string(c)
		if c == '\n' {
			toReplace = `\n`
		}
		if c == '\r' {
			toReplace = `\r`
		}
		line = strings.Replace(line, string(c), toReplace, -1)
	}
	return line
}
