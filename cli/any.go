package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nlpodyssey/gopickle/pickle"
	"github.com/nlpodyssey/gopickle/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

var (
	anyTypeStyle = lipgloss.NewStyle().Foreground(grayColor)
	anyNilStyle  = lipgloss.NewStyle().Foreground(grayColor)
	kwargStyle   = lipgloss.NewStyle().Foreground(grayColor)
)

func anyString(any *anypb.Any) string {
	if any == nil {
		return anyNilStyle.Render("nil")
	}
	switch any.TypeUrl {
	case "type.googleapis.com/google.protobuf.BytesValue":
		if s, err := anyBytesString(any); err == nil && s != "" {
			return s
		}
		// Suppress the error; render the type only.
	}
	return anyTypeStyle.Render(fmt.Sprintf("<%s>", any.TypeUrl))

}

func anyBytesString(any *anypb.Any) (string, error) {
	m, err := anypb.UnmarshalNew(any, proto.UnmarshalOptions{})
	if err != nil {
		return "", err
	}
	bv, ok := m.(*wrapperspb.BytesValue)
	if !ok {
		return "", fmt.Errorf("invalid bytes value: %T", m)
	}
	b := bv.Value

	// TODO: support unpacking other types of serialized values
	return pythonPickleString(b)
}

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
		return anyNilStyle.Render("nil"), nil
	case bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, float32, float64:
		return fmt.Sprintf("%v", v), nil
	case *pythonArgumentsObject:
		return pythonArgumentsString(v)
	default:
		return "", fmt.Errorf("unsupported Python value: %T", value)
	}
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
		for i, key := range a.kwargs.Keys() {
			if i > 0 || argsLen > 0 {
				b.WriteString(", ")
			}
			keyStr, err := pythonValueString(key)
			if err != nil {
				return "", err
			}
			b.WriteString(kwargStyle.Render(keyStr + "="))

			value := a.kwargs.MustGet(key)
			valueStr, err := pythonValueString(value)
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
