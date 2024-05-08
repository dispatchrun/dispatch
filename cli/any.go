package cli

import (
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func anyString(any *anypb.Any) string {
	if any == nil {
		return "nil"
	}
	switch any.TypeUrl {
	case "type.googleapis.com/google.protobuf.BytesValue":
		s, err := anyBytesString(any)
		if err != nil {
			slog.Debug("cannot parse input/output value", "error", err)
			// fallthrough
		} else {
			return s
		}
	}
	return fmt.Sprintf("%s(?)", any.TypeUrl)
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
