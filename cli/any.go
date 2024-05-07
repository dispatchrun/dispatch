package cli

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

var (
	anyTypeStyle = lipgloss.NewStyle().Foreground(grayColor)
	anyNilStyle  = lipgloss.NewStyle().Foreground(grayColor)
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
