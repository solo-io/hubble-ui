package common

import (
	"encoding/base64"
	"fmt"

	"github.com/cilium/cilium/api/v1/flow"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type FlowDecoder struct {
	Encoding string
}

func NewFlowDecoder(encoding string) (*FlowDecoder, error) {
	d := &FlowDecoder{
		Encoding: encoding,
	}

	if !d.validEncoding(encoding) {
		return nil, fmt.Errorf("encoding not supported: %s", encoding)
	}

	return d, nil
}

func (d *FlowDecoder) validEncoding(encoding string) bool {
	return slices.Contains(d.supportedEncodings(), encoding)
}

func (d *FlowDecoder) supportedEncodings() []string {
	return []string{
		EncodingJSON,
		EncodingJSONBASE64,
	}
}

func (d *FlowDecoder) FromValue(val string) (*flow.Flow, error) {
	var ciliumFlow flow.Flow

	valB := []byte(val)
	var err error
	switch d.Encoding {
	case EncodingJSONBASE64:
		valB, err = base64.RawStdEncoding.DecodeString(val)
		if err != nil {
			return nil, err
		}
		fallthrough
	case EncodingJSON:
		err := UnmarshalJSON(valB, &ciliumFlow)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
		}

		return &ciliumFlow, nil
	}

	return nil, fmt.Errorf("unsupported encoding: %s", d.Encoding)
}

var jsonUnmarshaler = &protojson.UnmarshalOptions{
	AllowPartial:   false,
	DiscardUnknown: false,
}

func UnmarshalJSON(b []byte, msg proto.Message) error {
	return jsonUnmarshaler.Unmarshal(b, msg)
}
