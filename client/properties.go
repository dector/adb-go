package client

import (
	"bufio"
	"context"
	"fmt"
	"strings"
)

// GetProp reads one Android system property by running getprop through the
// device shell. Missing properties are returned as an empty string, matching the
// device getprop command's behavior.
func (c *Client) GetProp(ctx context.Context, name string) (string, error) {
	out, err := c.Shell(ctx, "getprop "+shellQuote(name))
	if err != nil {
		return "", fmt.Errorf("adb getprop %q: %w", name, err)
	}
	value, err := parseGetPropValue(string(out))
	if err != nil {
		return "", fmt.Errorf("adb getprop %q: %w", name, err)
	}
	return value, nil
}

// Properties reads all Android system properties by running getprop through the
// device shell and parsing the standard "[name]: [value]" output format.
func (c *Client) Properties(ctx context.Context) (map[string]string, error) {
	out, err := c.Shell(ctx, "getprop")
	if err != nil {
		return nil, fmt.Errorf("adb getprop: %w", err)
	}
	props, err := parseGetPropOutput(string(out))
	if err != nil {
		return nil, fmt.Errorf("adb getprop: %w", err)
	}
	return props, nil
}

func parseGetPropValue(output string) (string, error) {
	output = strings.TrimSuffix(output, "\n")
	output = strings.TrimSuffix(output, "\r")
	if strings.ContainsAny(output, "\r\n") {
		return "", fmt.Errorf("unexpected multi-line property value output")
	}
	return output, nil
}

func parseGetPropOutput(output string) (map[string]string, error) {
	props := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		name, value, ok := parseGetPropLine(line)
		if !ok {
			return nil, fmt.Errorf("malformed getprop line %d: %q", lineNumber, line)
		}
		props[name] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read getprop output: %w", err)
	}
	return props, nil
}

func parseGetPropLine(line string) (name, value string, ok bool) {
	if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
		return "", "", false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
	name, value, found := strings.Cut(body, "]: [")
	if !found || name == "" {
		return "", "", false
	}
	return name, value, true
}
