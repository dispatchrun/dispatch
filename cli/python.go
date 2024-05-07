package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nlpodyssey/gopickle/pickle"
	"github.com/nlpodyssey/gopickle/types"
)

var (
	kwargStyle = lipgloss.NewStyle().Foreground(grayColor)
)

func pythonPickleString(b []byte) (string, error) {
	u := pickle.NewUnpickler(bytes.NewReader(b))
	u.FindClass = findPythonClass

	value, err := u.Load()
	if err != nil {
		return "", err
	}
	return pythonValueString(value)
}

func pythonValueString(value interface{}) (string, error) {
	switch v := value.(type) {
	case nil:
		return "None", nil
	case bool:
		if v {
			return "True", nil
		}
		return "False", nil
	case string:
		return fmt.Sprintf("%q", v), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, float32, float64:
		return fmt.Sprintf("%v", v), nil
	case *types.List:
		return pythonListString(v)
	case *types.Tuple:
		return pythonTupleString(v)
	case *types.Dict:
		return pythonDictString(v)
	case *types.Set:
		return pythonSetString(v)
	case *pythonArgumentsObject:
		return pythonArgumentsString(v)
	case *types.GenericClass:
		return fmt.Sprintf("%s.%s", v.Module, v.Name), nil
	case *types.GenericObject:
		s, _ := pythonValueString(v.Class)
		return fmt.Sprintf("%s(?)", s), nil
	default:
		return "", fmt.Errorf("unsupported Python value: %T", value)
	}
}

func pythonListString(list *types.List) (string, error) {
	var b strings.Builder
	b.WriteByte('[')
	for i, entry := range *list {
		if i > 0 {
			b.WriteString(", ")
		}
		s, err := pythonValueString(entry)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	b.WriteByte(']')
	return b.String(), nil
}

func pythonTupleString(tuple *types.Tuple) (string, error) {
	var b strings.Builder
	b.WriteByte('(')
	for i, entry := range *tuple {
		if i > 0 {
			b.WriteString(", ")
		}
		s, err := pythonValueString(entry)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	b.WriteByte(')')
	return b.String(), nil
}

func pythonDictString(dict *types.Dict) (string, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, entry := range *dict {
		if i > 0 {
			b.WriteString(", ")
		}
		keyStr, err := pythonValueString(entry.Key)
		if err != nil {
			return "", err
		}
		b.WriteString(keyStr)
		b.WriteString(": ")

		valueStr, err := pythonValueString(entry.Value)
		if err != nil {
			return "", err
		}
		b.WriteString(valueStr)
	}
	b.WriteByte('}')
	return b.String(), nil
}

func pythonSetString(set *types.Set) (string, error) {
	var b strings.Builder
	b.WriteByte('{')
	var i int
	for entry := range *set {
		if i > 0 {
			b.WriteString(", ")
		}
		s, err := pythonValueString(entry)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
		i++
	}
	b.WriteByte('}')
	return b.String(), nil
}

func pythonArgumentsString(a *pythonArgumentsObject) (string, error) {
	var b strings.Builder
	b.WriteByte('(')

	var argsLen int
	if a.args != nil {
		argsLen = a.args.Len()
		for i := 0; i < argsLen; i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			arg := a.args.Get(i)
			s, err := pythonValueString(arg)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		}
	}

	if a.kwargs != nil {
		for i, entry := range *a.kwargs {
			if i > 0 || argsLen > 0 {
				b.WriteString(", ")
			}
			var keyStr string
			if s, ok := entry.Key.(string); ok {
				keyStr = s
			} else {
				var err error
				keyStr, err = pythonValueString(entry.Key)
				if err != nil {
					return "", err
				}
			}
			b.WriteString(kwargStyle.Render(keyStr + "="))

			valueStr, err := pythonValueString(entry.Value)
			if err != nil {
				return "", err
			}
			b.WriteString(valueStr)
		}
	}

	b.WriteByte(')')
	return b.String(), nil

}

func findPythonClass(module, name string) (interface{}, error) {
	// https://github.com/dispatchrun/dispatch-py/blob/0a482491/src/dispatch/proto.py#L175
	if module == "dispatch.proto" && name == "Arguments" {
		return &pythonArgumentsClass{}, nil
	}
	return types.NewGenericClass(module, name), nil
}

type pythonArgumentsClass struct{}

func (a *pythonArgumentsClass) PyNew(args ...interface{}) (interface{}, error) {
	return &pythonArgumentsObject{}, nil
}

type pythonArgumentsObject struct {
	args   *types.Tuple
	kwargs *types.Dict
}

var _ types.PyDictSettable = (*pythonArgumentsObject)(nil)

func (a *pythonArgumentsObject) PyDictSet(key, value interface{}) error {
	var ok bool
	switch key {
	case "args":
		if a.args, ok = value.(*types.Tuple); !ok {
			return fmt.Errorf("invalid Arguments.args: %T", value)
		}
	case "kwargs":
		if a.kwargs, ok = value.(*types.Dict); !ok {
			return fmt.Errorf("invalid Arguments.kwargs: %T", value)
		}
	default:
		return fmt.Errorf("unexpected key: %v", key)
	}
	return nil
}
