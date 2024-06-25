package cli

import (
	"fmt"
	"log/slog"
	"strconv"

	pythonv1 "buf.build/gen/go/stealthrocket/dispatch-proto/protocolbuffers/go/dispatch/sdk/python/v1"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func anyString(any *anypb.Any) string {
	if any == nil {
		return "nil"
	}

	m, err := any.UnmarshalNew()
	if err != nil {
		return unsupportedAny(any, err)
	}

	switch mm := m.(type) {
	case *wrapperspb.BytesValue:
		// The Python SDK originally wrapped pickled values in a
		// wrapperspb.BytesValue. Try to unpickle the bytes first,
		// and return literal bytes if they cannot be unpickled.
		s, err := pythonPickleString(mm.Value)
		if err != nil {
			s = fmt.Sprintf("bytes(%s)", truncateBytes(mm.Value))
		}
		return s

	case *wrapperspb.Int32Value:
		return strconv.FormatInt(int64(mm.Value), 10)

	case *wrapperspb.Int64Value:
		return strconv.FormatInt(mm.Value, 10)

	case *wrapperspb.UInt32Value:
		return strconv.FormatUint(uint64(mm.Value), 10)

	case *wrapperspb.UInt64Value:
		return strconv.FormatUint(mm.Value, 10)

	case *wrapperspb.StringValue:
		return fmt.Sprintf("%q", mm.Value)

	case *wrapperspb.BoolValue:
		return strconv.FormatBool(mm.Value)

	case *wrapperspb.FloatValue:
		return fmt.Sprintf("%v", mm.Value)

	case *wrapperspb.DoubleValue:
		return fmt.Sprintf("%v", mm.Value)

	case *emptypb.Empty:
		return "empty()"

	case *timestamppb.Timestamp:
		return mm.AsTime().String()

	case *durationpb.Duration:
		return mm.AsDuration().String()

	case *pythonv1.Pickled:
		s, err := pythonPickleString(mm.PickledValue)
		if err != nil {
			return unsupportedAny(any, fmt.Errorf("pickle error: %w", err))
		}
		return s

	default:
		return unsupportedAny(any, fmt.Errorf("not implemented: %T", m))
	}
}

func unsupportedAny(any *anypb.Any, err error) string {
	if err != nil {
		slog.Debug("cannot parse input/output value", "error", err)
	}
	return fmt.Sprintf("%s(?)", any.TypeUrl)
}

func truncateBytes(b []byte) []byte {
	const n = 4
	if len(b) < n {
		return b
	}
	return append(b[:n:n], "..."...)
}
