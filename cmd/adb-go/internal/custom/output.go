package custom

import (
	"encoding/json"
	"fmt"
	"io"
)

func writeJSON(stdout io.Writer, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", body)
	return err
}
