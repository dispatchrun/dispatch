package cli

import (
	"testing"
	"time"

	pythonv1 "buf.build/gen/go/stealthrocket/dispatch-proto/protocolbuffers/go/dispatch/sdk/python/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestAnyString(t *testing.T) {
	for _, test := range []struct {
		input *anypb.Any
		want  string
	}{
		{
			input: asAny(wrapperspb.Bool(true)),
			want:  "true",
		},
		{
			input: asAny(wrapperspb.Int32(-1)),
			want:  "-1",
		},
		{
			input: asAny(wrapperspb.Int64(2)),
			want:  "2",
		},
		{
			input: asAny(wrapperspb.UInt32(3)),
			want:  "3",
		},
		{
			input: asAny(wrapperspb.UInt64(4)),
			want:  "4",
		},
		{
			input: asAny(wrapperspb.Float(1.25)),
			want:  "1.25",
		},
		{
			input: asAny(wrapperspb.Double(3.14)),
			want:  "3.14",
		},
		{
			input: asAny(wrapperspb.String("foo")),
			want:  `"foo"`,
		},
		{
			input: asAny(wrapperspb.Bytes([]byte("foobar"))),
			want:  "bytes(foob...)",
		},
		{
			input: asAny(timestamppb.New(time.Date(2024, time.June, 25, 10, 56, 11, 1234, time.UTC))),
			want:  "2024-06-25 10:56:11.000001234 +0000 UTC",
		},
		{
			input: asAny(durationpb.New(1 * time.Second)),
			want:  "1s",
		},
		{
			// $ python3 -c 'import pickle; print(pickle.dumps(1))'
			// b'\x80\x04K\x01.'
			input: pickled([]byte("\x80\x04K\x01.")),
			want:  "1",
		},
		{
			// Legacy way that the Python SDK wrapped pickled values:
			input: asAny(wrapperspb.Bytes([]byte("\x80\x04K\x01."))),
			want:  "1",
		},
		{
			// $ python3 -c 'import pickle; print(pickle.dumps("bar"))'
			// b'\x80\x04\x95\x07\x00\x00\x00\x00\x00\x00\x00\x8c\x03foo\x94.'
			input: pickled([]byte("\x80\x04\x95\x07\x00\x00\x00\x00\x00\x00\x00\x8c\x03bar\x94.")),
			want:  `"bar"`,
		},
		{
			input: pickled([]byte("!!!invalid!!!")),
			want:  "buf.build/stealthrocket/dispatch-proto/dispatch.sdk.python.v1.Pickled(?)",
		},
		{
			input: &anypb.Any{TypeUrl: "com.example/some.Message"},
			want:  "com.example/some.Message(?)",
		},
		{
			input: asAny(&emptypb.Empty{}),
			want:  "empty()",
		},
	} {
		t.Run(test.want, func(*testing.T) {
			got := anyString(test.input)
			if got != test.want {
				t.Errorf("unexpected string: got %v, want %v", got, test.want)
			}
		})
	}
}

func asAny(m proto.Message) *anypb.Any {
	any, err := anypb.New(m)
	if err != nil {
		panic(err)
	}
	return any
}

func pickled(b []byte) *anypb.Any {
	m := &pythonv1.Pickled{PickledValue: b}
	mb, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return &anypb.Any{
		TypeUrl: "buf.build/stealthrocket/dispatch-proto/" + string(m.ProtoReflect().Descriptor().FullName()),
		Value:   mb,
	}
}
