package chatgpt

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type OrderedMap struct {
	Keys   []string
	Values map[string]interface{}
}

func NewOrderedMap() *OrderedMap {
	return &OrderedMap{
		Values: make(map[string]interface{}),
	}
}

func (o *OrderedMap) Add(key string, value interface{}) {
	if _, exists := o.Values[key]; !exists {
		o.Keys = append(o.Keys, key)
	}
	o.Values[key] = value
}

func (o *OrderedMap) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString("{")
	length := len(o.Keys)
	for i, key := range o.Keys {
		jsonValue, err := json.Marshal(o.Values[key])
		if err != nil {
			return nil, err
		}
		buffer.WriteString(fmt.Sprintf("\"%s\":%s", key, jsonValue))
		if i < length-1 {
			buffer.WriteString(",")
		}
	}
	buffer.WriteString("}")
	return buffer.Bytes(), nil
}

type turnTokenList [][]any

func getTurnstileToken(dx, p string) (string, error) {
	decodedBytes, err := base64.StdEncoding.DecodeString(dx)
	if err != nil {
		return "", err
	}
	return porcessTurnstileToken(string(decodedBytes), p)
}
func porcessTurnstileToken(dx, p string) (string, error) {
	var result []rune
	dxRunes := []rune(dx)
	pRunes := []rune(p)
	pLength := len(pRunes)
	if pLength != 0 {
		for i, r := range dxRunes {
			result = append(result, r^pRunes[i%pLength])
		}
	} else {
		result = dxRunes
	}
	return string(result), nil
}

type FuncType func(args ...any) any

type FloatMap map[float64]any

type StringMap map[string]any

func isSlice(input any) bool {
	return reflect.TypeOf(input).Kind() == reflect.Slice
}
func isFloat64(input any) bool {
	_, ok := input.(float64)
	return ok
}
func isString(input any) bool {
	_, ok := input.(string)
	return ok
}
func toStr(input any) string {
	var output string
	if input == nil {
		output = "undefined"
	} else if isFloat64(input) {
		output = strconv.FormatFloat(input.(float64), 'f', -1, 64)
	} else if isString(input) {
		inputStr := input.(string)
		if inputStr == "window.Math" {
			output = "[object Math]"
		} else if inputStr == "window.Reflect" {
			output = "[object Reflect]"
		} else if inputStr == "window.performance" {
			output = "[object Performance]"
		} else if inputStr == "window.localStorage" {
			output = "[object Storage]"
		} else if inputStr == "window.Object" {
			output = "function Object() { [native code] }"
		} else if inputStr == "window.Reflect.set" {
			output = "function set() { [native code] }"
		} else if inputStr == "window.performance.now" {
			output = "function () { [native code] }"
		} else if inputStr == "window.Object.create" {
			output = "function create() { [native code] }"
		} else if inputStr == "window.Object.keys" {
			output = "function keys() { [native code] }"
		} else if inputStr == "window.Math.random" {
			output = "function random() { [native code] }"
		} else {
			output = inputStr
		}
	} else if inputArray, ok := input.([]string); ok {
		output = strings.Join(inputArray, ",")
	} else {
		fmt.Printf("Type of input is: %s\n", reflect.TypeOf(input))
	}
	return output
}

func getFuncMap() FloatMap {
	var processMap FloatMap = FloatMap{}
	processMap[1] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		estr := toStr(processMap[e])
		tstr := toStr(processMap[t])
		res, err := porcessTurnstileToken(estr, tstr)
		if err != nil {
			fmt.Println(err)
		}
		processMap[e] = res
		return nil
	})
	processMap[2] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1]
		processMap[e] = t
		return nil
	})
	processMap[5] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		n := processMap[e]
		tres := processMap[t]
		if isSlice(n) {
			nt := n.([]any)
			nt = append(nt, tres)
			processMap[e] = nt
		} else {
			var res any
			if isString(n) || isString(tres) {
				nstr := toStr(n)
				tstr := toStr(tres)
				res = nstr + tstr
			} else if isFloat64(n) && isFloat64(tres) {
				nnum := n.(float64)
				tnum := tres.(float64)
				res = nnum + tnum
			} else {
				res = "NaN"
			}
			processMap[e] = res
		}
		return nil
	})
	processMap[6] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		n := args[2].(float64)
		tv := processMap[t]
		nv := processMap[n]
		if isString(tv) && isString(nv) {
			tstr := tv.(string)
			nstr := nv.(string)
			res := tstr + "." + nstr
			if res == "window.document.location" {
				processMap[e] = "https://chatgpt.com/"
			} else {
				processMap[e] = res
			}
		} else {
			fmt.Println("func type 6 error")
		}
		return nil
	})
	processMap[24] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		n := args[2].(float64)
		tv := processMap[t]
		nv := processMap[n]
		if isString(tv) && isString(nv) {
			tstr := tv.(string)
			nstr := nv.(string)
			processMap[e] = tstr + "." + nstr
		} else {
			fmt.Println("func type 24 error")
		}
		return nil
	})
	processMap[7] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := len(args[1:])
		n := []any{}
		for i := range t {
			v := args[i+1].(float64)
			vv := processMap[v]
			n = append(n, vv)
		}
		ev := processMap[e]
		switch ev := ev.(type) {
		case string:
			if ev == "window.Reflect.set" {
				object := n[0].(*OrderedMap)
				keyStr := strconv.FormatFloat(n[1].(float64), 'f', -1, 64)
				val := n[2]
				object.Add(keyStr, val)
			}
		case FuncType:
			ev(n...)
		}
		return nil
	})
	processMap[17] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		n := len(args[2:])
		i := []any{}
		for idx := range n {
			v := args[idx+2].(float64)
			vv := processMap[v]
			i = append(i, vv)
		}
		tv := processMap[t]
		var res any
		switch tv := tv.(type) {
		case string:
			if tv == "window.performance.now" {
				res = (float64(time.Since(startTime).Nanoseconds()) + rand.Float64()) / 1e6
			} else if tv == "window.Object.create" {
				res = NewOrderedMap()
			} else if tv == "window.Object.keys" {
				if input, ok := i[0].(string); ok {
					if input == "window.localStorage" {
						res = []string{"STATSIG_LOCAL_STORAGE_INTERNAL_STORE_V4", "STATSIG_LOCAL_STORAGE_STABLE_ID", "client-correlated-secret", "oai/apps/capExpiresAt", "oai-did", "STATSIG_LOCAL_STORAGE_LOGGING_REQUEST", "UiState.isNavigationCollapsed.1"}
					}
				}
			} else if tv == "window.Math.random" {
				rand.NewSource(time.Now().UnixNano())
				res = rand.Float64()
			}
		case FuncType:
			res = tv(i...)
		}
		processMap[e] = res
		return nil
	})
	processMap[8] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		tv := processMap[t]
		processMap[e] = tv
		return nil
	})
	processMap[10] = "window"
	processMap[14] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		tv := processMap[t]
		if isString(tv) {
			var tokenList turnTokenList
			err := json.Unmarshal([]byte(tv.(string)), &tokenList)
			if err != nil {
				fmt.Println(err)
			}
			processMap[e] = tokenList
		} else {
			fmt.Println("func type 14 error")
		}
		return nil
	})
	processMap[15] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		tv := processMap[t]
		tres, err := json.Marshal(tv)
		if err != nil {
			fmt.Println(err)
		}
		processMap[e] = string(tres)
		return nil
	})
	processMap[18] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		ev := processMap[e]
		estr := toStr(ev)
		decoded, err := base64.StdEncoding.DecodeString(estr)
		if err != nil {
			fmt.Println(err)
		}
		processMap[e] = string(decoded)
		return nil
	})
	processMap[19] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		ev := processMap[e]
		estr := toStr(ev)
		encoded := base64.StdEncoding.EncodeToString([]byte(estr))
		processMap[e] = encoded
		return nil
	})
	processMap[20] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		n := args[2].(float64)
		i := len(args[3:])
		o := []any{}
		for idx := range i {
			v := args[idx+3].(float64)
			vv := processMap[v]
			o = append(o, vv)
		}
		ev := processMap[e]
		tv := processMap[t]
		if ev == tv {
			nv := processMap[n]
			switch nv := nv.(type) {
			case FuncType:
				nv(o...)
			default:
				fmt.Println("func type 20 error")
			}
		}
		return nil
	})
	processMap[21] = FuncType(func(args ...any) any {
		return nil
	})
	processMap[23] = FuncType(func(args ...any) any {
		e := args[0].(float64)
		t := args[1].(float64)
		n := len(args[2:])
		i := []any{}
		for idx := range n {
			v := args[idx+2].(float64)
			i = append(i, v)
		}
		ev := processMap[e]
		tv := processMap[t]
		if ev != nil {
			switch tv := tv.(type) {
			case FuncType:
				tv(i...)
			}
		}
		return nil
	})
	return processMap
}

func ProcessTurnstile(dx, p string) string {
	// current don't process turnstile
	return ""
	tokens, _ := getTurnstileToken(dx, p)
	var tokenList turnTokenList
	err := json.Unmarshal([]byte(tokens), &tokenList)
	if err != nil {
		fmt.Println(err)
	}
	var res string
	processMap := getFuncMap()
	processMap[3] = FuncType(func(args ...any) any {
		e := args[0].(string)
		res = base64.StdEncoding.EncodeToString([]byte(e))
		return nil
	})
	processMap[9] = tokenList
	processMap[16] = p
	for len(processMap[9].(turnTokenList)) > 0 {
		list := processMap[9].(turnTokenList)
		token := list[0]
		processMap[9] = list[1:]
		e := token[0].(float64)
		t := token[1:]
		f := processMap[e].(FuncType)
		f(t...)
	}
	return res
}
