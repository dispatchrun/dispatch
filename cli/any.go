package cli

import (
	"fmt"
	"log/slog"

	pythonv1 "buf.build/gen/go/stealthrocket/dispatch-proto/protocolbuffers/go/dispatch/sdk/python/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func anyString(any *anypb.Any) string {
	if any == nil {
		return "nil"
	}

	var s string
	var err error
	switch any.TypeUrl {
	case "buf.build/stealthrocket/dispatch-proto/dispatch.sdk.python.v1.Pickled":
		var pickled proto.Message
		pickled, err = any.UnmarshalNew()
		if err == nil {
			if p, ok := pickled.(*pythonv1.Pickled); ok {
				s, err = pythonPickleString(p.PickledValue)
			} else {
				err = fmt.Errorf("invalid pickled message: %T", p)
			}
		}
	case "type.googleapis.com/google.protobuf.BytesValue":
		s, err = anyBytesString(any)
	default:
		// TODO: support unpacking other types of serialized values
		err = fmt.Errorf("not implemented: %s", any.TypeUrl)
	}
	if err != nil {
		slog.Debug("cannot parse input/output value", "error", err)
		return fmt.Sprintf("%s(?)", any.TypeUrl)
	}
	return s
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

	// The Python SDK originally wrapped pickled values in a
	// wrapperspb.BytesValue. Try to unpickle the bytes first,
	// and return literal bytes if they cannot be unpickled.
	s, err := pythonPickleString(b)
	if err != nil {
		s = string(truncateBytes(b))
	}
	return s, nil
}

func truncateBytes(b []byte) []byte {
	const n = 4
	if len(b) < n {
		return b
	}
	return append(b[:n:n], "..."...)
}
