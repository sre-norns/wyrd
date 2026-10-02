package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gopkg.in/yaml.v3"
)

// MaxInput is the largest resource document read from a file or standard input.
const MaxInput = 1 << 20

// InputFlag names a resource document. Embed it in commands that write one.
type InputFlag struct {
	File string `required:"" short:"f" help:"JSON or YAML resource file, or - for standard input"`
}

// ReadInput reads a document from file, or from stdin for "-".
func ReadInput(file string, stdin io.Reader) ([]byte, error) {
	reader := stdin
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		reader = f
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxInput+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxInput {
		return nil, fmt.Errorf("input exceeds 1 MiB")
	}
	return data, nil
}

// DecodeObject decodes one JSON or YAML object into T, refusing fields T does
// not have: a misspelt field is an error, not silently dropped. It also returns
// the fields the document set, so an update can send only those.
func DecodeObject[T any](data []byte) (value T, fields map[string]json.RawMessage, err error) {
	data, err = asJSON(data)
	if err != nil {
		return value, nil, err
	}
	if resource.IsResource(value) {
		fields, err = resource.DecodeDocument(data, &value)
		return value, fields, err
	}
	if err = json.Unmarshal(data, &fields); err != nil {
		return value, nil, err
	}
	if fields == nil {
		return value, nil, fmt.Errorf("input must be an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&value)
	return value, fields, err
}

// asJSON returns a YAML document as JSON; a JSON document is returned as is.
func asJSON(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return data, nil
	}
	var document any
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("input is neither JSON nor YAML: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("input must contain one YAML document")
	}
	return json.Marshal(document)
}

// ListFlags are the common flags of list commands.
type ListFlags struct {
	Fields   string `help:"Resource field selector, e.g. status.phase=active"`
	Cursor   string `help:"Continue a list from the cursor a previous page printed"`
	Limit    uint   `help:"Maximum number of resources per page" default:"100"`
	All      bool   `help:"Follow every page to the end of the list"`
	Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`
}

// SearchQuery is the query the flags describe.
func (f *ListFlags) SearchQuery() (manifest.SearchQuery, error) {
	q, err := SearchQuery(f.Selector)
	if err == nil {
		q.Fields, err = manifest.ParseSelector(f.Fields)
	}
	q.Cursor, q.Limit = f.Cursor, f.Limit
	return q, err
}

// ListPages lists with the flags and prints the result: one page, or with
// --all every page to the end.
func ListPages[T any](ctx context.Context, out Output, flags ListFlags, list func(context.Context, manifest.SearchQuery) ([]T, manifest.Page, error)) error {
	query, err := flags.SearchQuery()
	if err != nil {
		return err
	}
	results, page, err := list(ctx, query)
	for err == nil && flags.All && page.Next != "" {
		query.Cursor = page.Next
		more, next, moreErr := list(ctx, query)
		results, page, err = append(results, more...), next, moreErr
	}
	if err != nil {
		return err
	}
	return RenderList(out, results, page)
}

// SearchQuery is a query for the resources a label selector matches.
func SearchQuery(selector string) (manifest.SearchQuery, error) {
	parsed, err := manifest.ParseSelector(selector)
	if err != nil {
		return manifest.SearchQuery{}, fmt.Errorf("failed to parse labels selector: %w", err)
	}

	return manifest.SearchQuery{Selector: parsed}, nil
}
