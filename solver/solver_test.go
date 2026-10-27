package solver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/ypsu/efftesting/efft"
	"github.com/ypsu/sdin"
	"github.com/ypsu/textar"
)

func TestLimits(t *testing.T) {
	efft.Init(t)

	inputdata, err := os.ReadFile("../testcases.textar")
	if err != nil {
		inputdata = efft.Must1(os.ReadFile("testcases.textar"))
	}
	inputs := textar.Parse(inputdata).Files
	outputdata, errordata := Solve(EncodeTestcases(inputs))
	outputs, errors := strings.Split(string(outputdata), "==NextAnswer==\n")[1:], strings.Split(string(errordata), "==NextNotes==\n")[1:]
	if len(inputs) != len(outputs) || len(inputs) != len(errors) {
		t.Fatalf("solvertest.BadResultSize inputs=%d outputs=%d errors=%d", len(inputs), len(outputs), len(errors))
	}

	var maxInputLen int
	var maxInputTestcase string
	var tcnames []string
	spaceMatcher := regexp.MustCompile("[ \t\n]")
	for i, input := range inputs {
		efft.Note = input.Name
		_, tcname, _ := strings.Cut(input.Name, ".")
		if !unicode.IsUpper(rune(tcname[0])) {
			t.Errorf("solvertest.TestcaseNoUppercaseStart name=%q", input.Name)
		}
		for _, c := range tcname {
			if !(unicode.IsDigit(c) || unicode.IsLetter(c)) {
				t.Errorf("solvertest.TestcaseInvalidCharacter name=%q character=%q", input.Name, string(c))
			}
		}
		tcnames = append(tcnames, input.Name)
		if input.Name != "fmt.CrazyLine" {
			for line := range strings.Lines(string(input.Data)) {
				line = strings.TrimSuffix(line, "\n")
				if len(line) > maxInputLen {
					maxInputLen, maxInputTestcase = len(line), input.Name
				}
			}
		}
		if !strings.HasPrefix(outputs[i], "ERROR") && strings.HasPrefix(input.Name, "tojson.") && !json.Valid([]byte(outputs[i])) {
			t.Fatalf("solvertest.InvalidJSON tc=%s output:\n%s", input.Name, outputs[i])
		}
		for _, ch := range input.Data {
			if ch >= 128 {
				t.Errorf("solvertest.UnexpectedUTF8 tc=%s", input.Name)
				break
			}
		}
		if strings.HasPrefix(input.Name, "fmt.") &&
			!strings.HasPrefix(outputs[i], "ERROR") &&
			!strings.Contains(outputs[i], "sdin.KeepSorted") &&
			!strings.Contains(outputs[i], "'''sdin") &&
			input.Name != "fmt.MapScalarFieldComment" && input.Name != "fmt.MapListFieldComment" {

			strippedInput := spaceMatcher.ReplaceAll(input.Data, nil)
			strippedOutput := spaceMatcher.ReplaceAll([]byte(outputs[i]), nil)
			if bytes.Compare(strippedInput, strippedOutput) != 0 {
				t.Errorf("solvertest.InvalidFormatResult testcase=%s\n## input:\n%s\n## output:\n%s", input.Name, input.Data, outputs[i])
			}
		}

		task, _, _ := strings.Cut(input.Name, ".")
		if task == "forms" || task == "schema" || task == "tojson" && !strings.Contains(input.Name, "Tab") {
			nt := sdin.TFlatStruct()
			if task == "forms" {
				nt = sdin.TFlatForm()
			}
			n, err := sdin.Parse(nt, string(input.Data))
			if err != nil {
				continue
			}
			f := strings.ReplaceAll(n.Format(), "\t", "  ")
			if string(input.Data) != f {
				gotlines, wantlines := strings.Split(string(input.Data), "\n"), strings.Split(f, "\n")
				badline := min(len(wantlines), len(gotlines))
				for i := range badline {
					if wantlines[i] != gotlines[i] {
						t.Errorf("solvertest.DifferentLines testcase=%s:\n got: %q\nwant: %q", input.Name, strings.ReplaceAll(gotlines[i], " ", "·"), strings.ReplaceAll(wantlines[i], " ", "·"))
						badline = i + 1
						break
					}
				}
				t.Errorf("solvertest.UnformattedTestcase testcase=%s diffline=%d want:\n%s", input.Name, badline, f)
			}
		}
		if task == "fmt" {
			n1, err := sdin.Parse(sdin.TFlatStruct(), string(input.Data))
			if err == nil { // on success
				f1 := n1.Format()
				n2 := efft.Must1(sdin.Parse(sdin.TFlatStruct(), f1))
				f2 := n2.Format()
				if f1 != f2 {
					t.Errorf("solvertest.NonIdempotentFormat testcase=%s", input.Name)
				}
			}
		}

		if strings.HasPrefix(input.Name, "done.") {
			break
		}
	}
	efft.Note = ""

	// Test max input len.
	efft.Effect(fmt.Sprintf("%3d %s", maxInputLen, maxInputTestcase)).Equals(" 89 tojson.AllowedDirectivePackages")
	if maxInputLen > 90 {
		t.Errorf("solvertest.InputLimitExceeded len=%d tc=%s", maxInputLen, maxInputTestcase)
	}

	for i, tc := range tcnames {
		for _, prevtc := range tcnames[:i] {
			if strings.HasPrefix(tc, prevtc) || strings.HasPrefix(prevtc, tc) ||
				strings.HasSuffix(tc, prevtc) || strings.HasSuffix(prevtc, tc) {
				t.Errorf("solvertest.PrefixConflict tc1=%s tc2=%s", tc, prevtc)
			}
		}
	}
}
