// Command openapi writes the generated public API contract.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/fadhln/pomkita-be/internal/httpapi"
)

func main() {
	check := flag.Bool("check", false, "check that the committed contract is current")
	output := flag.String("output", "docs/openapi.json", "OpenAPI output path")
	flag.Parse()

	document, err := json.MarshalIndent(httpapi.OpenAPIDocument(), "", "  ")
	if err != nil {
		fail("marshal OpenAPI document", err)
	}
	document = append(document, '\n')

	if *check {
		current, readErr := os.ReadFile(*output)
		if readErr != nil {
			fail("read OpenAPI contract", readErr)
		}
		if !bytes.Equal(current, document) {
			fail("OpenAPI contract is out of date", nil)
		}
		return
	}
	if err := os.WriteFile(*output, document, 0o644); err != nil {
		fail("write OpenAPI contract", err)
	}
}

func fail(action string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", action, err)
	} else {
		fmt.Fprintln(os.Stderr, action)
	}
	os.Exit(1)
}
