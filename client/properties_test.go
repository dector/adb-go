package client

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestGetPropReadsOnePropertyAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	opened := make(chan string, 1)
	server.Handle("shell:getprop 'ro.product.model'", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		opened <- string(open.Payload[:len(open.Payload)-1])
		writeServiceOutput(t, "Pixel 8\n")(ctx, conn, open)
	})

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	value, err := client.GetProp(context.Background(), "ro.product.model")
	if err != nil {
		t.Fatalf("GetProp() error = %v", err)
	}
	if value != "Pixel 8" {
		t.Fatalf("GetProp() = %q, want Pixel 8", value)
	}
	if got := <-opened; got != "shell:getprop 'ro.product.model'" {
		t.Fatalf("opened service = %q, want shell:getprop 'ro.product.model'", got)
	}
}

func TestPropertiesParsesAllPropertiesAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:getprop", writeServiceOutput(t, strings.Join([]string{
		"[ro.product.model]: [Pixel 8]",
		"[ro.build.version.sdk]: [35]",
		"[persist.demo.empty]: []",
		"[persist.demo.bracket]: [value with ] bracket]",
		"",
	}, "\n")))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	props, err := client.Properties(context.Background())
	if err != nil {
		t.Fatalf("Properties() error = %v", err)
	}
	want := map[string]string{
		"ro.product.model":     "Pixel 8",
		"ro.build.version.sdk": "35",
		"persist.demo.empty":   "",
		"persist.demo.bracket": "value with ] bracket",
	}
	if !reflect.DeepEqual(props, want) {
		t.Fatalf("Properties() = %#v, want %#v", props, want)
	}
}

func TestParseGetPropOutputEmpty(t *testing.T) {
	props, err := parseGetPropOutput("")
	if err != nil {
		t.Fatalf("parseGetPropOutput(empty) error = %v", err)
	}
	if len(props) != 0 {
		t.Fatalf("parseGetPropOutput(empty) = %#v, want empty map", props)
	}
}

func TestParseGetPropOutputMalformed(t *testing.T) {
	_, err := parseGetPropOutput("[ro.product.model]: [Pixel]\nnot a property line\n")
	if err == nil {
		t.Fatal("parseGetPropOutput(malformed) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "malformed getprop line 2") {
		t.Fatalf("parseGetPropOutput(malformed) error = %v, want line context", err)
	}
}

func TestParseGetPropValueEmptyAndMalformed(t *testing.T) {
	for _, output := range []string{"", "\n", "\r\n"} {
		value, err := parseGetPropValue(output)
		if err != nil {
			t.Fatalf("parseGetPropValue(%q) error = %v", output, err)
		}
		if value != "" {
			t.Fatalf("parseGetPropValue(%q) = %q, want empty", output, value)
		}
	}

	if _, err := parseGetPropValue("one\ntwo\n"); err == nil {
		t.Fatal("parseGetPropValue(multi-line) error = nil, want error")
	}
}
