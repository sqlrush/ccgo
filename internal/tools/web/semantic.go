package webtools

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
)

var webSemanticNumberLiteralRE = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func decodeWebStrictObject(raw json.RawMessage, allowed map[string]struct{}) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	return obj, nil
}

func coerceWebSemanticJSONNumbers(obj map[string]json.RawMessage, numberKeys map[string]struct{}) {
	for key, raw := range obj {
		if len(raw) == 0 || raw[0] != '"' {
			continue
		}
		if _, ok := numberKeys[key]; !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			continue
		}
		if webSemanticNumberLiteralRE.MatchString(text) {
			obj[key] = webSemanticJSONNumberRaw(text)
		}
	}
}

func webSemanticJSONNumberRaw(text string) json.RawMessage {
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return json.RawMessage(text)
	}
	if math.Trunc(number) == number {
		return json.RawMessage(strconv.FormatInt(int64(number), 10))
	}
	return json.RawMessage(strconv.FormatFloat(number, 'f', -1, 64))
}
