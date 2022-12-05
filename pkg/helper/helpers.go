package helper

import (
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/google/uuid"
	"github.com/openshift/rosa/pkg/reporter"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// ASCII codes of important characters:
const (
	aCode    = 97
	zCode    = 122
	zeroCode = 48
	nineCode = 57
)

// Number of letters and digits:
const (
	letterCount = zCode - aCode + 1
	digitCount  = nineCode - zeroCode + 1
)

func RandomLabel(size int) string {
	value := rand.Int() // #nosec G404
	chars := make([]byte, size)
	for size > 0 {
		size--
		if size%2 == 0 {
			chars[size] = byte(aCode + value%letterCount)
			value = value / letterCount
		} else {
			chars[size] = byte(zeroCode + value%digitCount)
			value = value / digitCount
		}
	}
	return string(chars)
}

func RankMapStringInt(values map[string]int) []string {
	type kv struct {
		Key   string
		Value int
	}
	var ss []kv
	for k, v := range values {
		ss = append(ss, kv{k, v})
	}
	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Value > ss[j].Value
	})
	ranked := make([]string, len(values))
	for i, kv := range ss {
		ranked[i] = kv.Key
	}
	sort.Slice(ranked, func(i, j int) bool {
		l1, l2 := len(ranked[i]), len(ranked[j])
		if l1 != l2 {
			return l1 > l2
		}
		return ranked[i] > ranked[j]
	})
	return ranked
}

func Contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func SliceToMap(s []string) map[string]bool {
	m := make(map[string]bool)

	for _, v := range s {
		m[v] = true
	}

	return m
}

func SliceToString(s []string) string {
	sort.Slice(s, func(i, j int) bool {
		l1, l2 := len(s[i]), len(s[j])
		if l1 != l2 {
			return l1 < l2
		}
		return s[i] < s[j]
	})
	return "[" + strings.Join(s, ", ") + "]"
}

func MapKeysToString(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return SliceToString(keys)
}

// RemoveStrFromSlice removes one occurrence of 'str' from the 's' slice if exists.
func RemoveStrFromSlice(s []string, str string) []string {
	for i, v := range s {
		if v == str {
			return append(s[:i], s[i+1:]...)
		}
	}

	return s
}

func DisplaySpinnerWithDelay(reporter *reporter.Object, infoMessage string, delay time.Duration) {
	if reporter.IsTerminal() {
		spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
		reporter.Infof(infoMessage)
		spin.Start()
		time.Sleep(delay)
		spin.Stop()
	} else {
		time.Sleep(delay)
	}
}

func SaveDocument(doc, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(doc)
	if err != nil {
		return err
	}

	return nil
}

func IsValidUUID(u string) bool {
	_, err := uuid.Parse(u)
	return err == nil
}

func HandleEscapedEmptyString(input string) string {
	if input == "\"\"" {
		input = ""
	}
	return input
}

func HandleEmptyStringOnSlice(slice []string) []string {
	r := []string{}
	for _, s := range slice {
		if s != "" {
			r = append(r, s)
		}
	}
	return r
}

func TrimUpToSuffix(orig, sufix string) string {
	for i := len(sufix); i >= 0; i-- {
		if strings.HasSuffix(orig, sufix[:i]) {
			return orig[:len(orig)-i]
		}
	}
	return orig
}
