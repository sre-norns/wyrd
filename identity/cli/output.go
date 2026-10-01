package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gopkg.in/yaml.v3"
)

// Output formats.
const (
	FormatTable = "table"
	FormatWide  = "wide"
	FormatYAML  = "yaml"
	FormatJSON  = "json"
)

// OutputFlag is the global -o flag. Embed it in a product's root flags.
type OutputFlag struct {
	Output string `help:"Output format: table, wide, yaml or json" short:"o" aliases:"format" enum:"table,wide,yaml,yml,json" default:"table"`
}

// Output is where and how results are printed.
type Output struct {
	Format string
	Stdout io.Writer
}

// NewOutput is an Output of format to standard output.
func NewOutput(format string) Output {
	if format == "yml" {
		format = FormatYAML
	}
	return Output{Format: format}
}

func (o Output) stdout() io.Writer {
	if o.Stdout != nil {
		return o.Stdout
	}
	return os.Stdout
}

// Wide reports whether tables print every column.
func (o Output) Wide() bool { return o.Format == FormatWide }

// Structured reports whether results are printed as documents rather than a
// table: nothing else, such as a page footer, may be printed beside them.
func (o Output) Structured() bool { return o.Format == FormatYAML || o.Format == FormatJSON }

// Encode prints value as a document in the output format. YAML is the JSON
// document re-rendered, so its keys are the wire's and can be applied back.
func (o Output) Encode(value any) error {
	switch o.Format {
	case FormatJSON:
		encoder := json.NewEncoder(o.stdout())
		encoder.SetIndent("", "\t")
		return encoder.Encode(value)
	case FormatYAML:
		data, err := toYAML(value)
		if err != nil {
			return err
		}
		_, err = o.stdout().Write(data)
		return err
	}
	return fmt.Errorf("unexpected output format %q", o.Format)
}

// toYAML renders the JSON encoding of value as block YAML, keeping the JSON
// field order -- unless value, or each element of a list, says how it is
// written as YAML. A product whose `apply` reads YAML with its own decoder
// needs what its encoder writes: a duration is "3s" there, and nanoseconds in
// its JSON.
func toYAML(value any) ([]byte, error) {
	if marshalsOwnYAML(value) {
		var out bytes.Buffer
		encoder := yaml.NewEncoder(&out)
		encoder.SetIndent(2)
		if err := encoder.Encode(value); err != nil {
			return nil, err
		}
		return out.Bytes(), encoder.Close()
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err = yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	blockStyle(&node)
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err = encoder.Encode(&node); err != nil {
		return nil, err
	}
	return out.Bytes(), encoder.Close()
}

var yamlMarshaler = reflect.TypeFor[yaml.Marshaler]()

func marshalsOwnYAML(value any) bool {
	t := reflect.TypeOf(value)
	if t == nil {
		return false
	}
	if t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	return t.Implements(yamlMarshaler)
}

func blockStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		blockStyle(child)
	}
}

// TableRower lets a type declare its own kubectl-style columns instead of the
// reflected ones.
type TableRower interface {
	TableHeader(wide bool) []string
	TableRow(wide bool) []any
}

// resourceMeta is satisfied by *T for any type embedding model.Resource.
type resourceMeta interface {
	Metadata() *model.Resource
}

// maxShortColumns caps the number of type-specific columns in short output.
const maxShortColumns = 3

// RenderList prints one page of a list, and how to fetch the next.
func RenderList[T any](out Output, results []T, page manifest.Page) error {
	if out.Structured() {
		return out.Encode(results)
	}
	w := out.stdout()
	renderTable(w, out.Wide(), results)
	if page.Total != nil {
		fmt.Fprintf(w, "%d of %d resources shown\n", len(results), *page.Total)
	} else {
		fmt.Fprintf(w, "%d resources shown\n", len(results))
	}
	if page.Next != "" {
		fmt.Fprintf(w, "more: --cursor %s (or --all)\n", page.Next)
	}

	return nil
}

func RenderResource[T any](out Output, resource T, err error) error {
	if err != nil {
		return err
	}
	if out.Structured() {
		return out.Encode(resource)
	}
	renderTable(out.stdout(), out.Wide(), []T{resource})

	return nil
}

// RenderFound prints a resource a lookup found, or reports that it did not.
func RenderFound[T any](out Output, resource T, exists bool, err error) error {
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("resource not found")
	}

	return RenderResource(out, resource, nil)
}

// RenderUpsert prints a resource a write created or updated.
func RenderUpsert[T any](out Output, resource T, created bool, err error) error {
	if err != nil {
		return err
	}
	if out.Structured() {
		return out.Encode(resource)
	}
	if created {
		fmt.Fprintln(out.stdout(), "created")
	} else {
		fmt.Fprintln(out.stdout(), "updated")
	}

	return RenderResource(out, resource, nil)
}

// renderTable prints rows as a kubectl-style borderless table.
func renderTable[T any](w io.Writer, wide bool, rows []T) {
	if len(rows) == 0 {
		return
	}

	header, extract := tableSchema[T](reflect.TypeOf(rows[0]), wide)
	if header == nil {
		// Not a struct we can reflect into; fall back to a plain dump.
		for i := range rows {
			fmt.Fprintf(w, "%+v\n", rows[i])
		}
		return
	}

	table := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, strings.Join(header, "\t"))
	slice := reflect.ValueOf(rows)
	for i := 0; i < slice.Len(); i++ {
		// Slice elements are addressable, so *T method sets (e.g. Metadata) apply.
		row := extract(slice.Index(i).Addr())
		cells := make([]string, len(row))
		for j, cell := range row {
			cells[j] = fmt.Sprint(cell)
		}
		fmt.Fprintln(table, strings.Join(cells, "\t"))
	}
	table.Flush()
}

// tableSchema builds the header and a per-element row extractor for T. It
// returns a nil header when T is not a renderable struct.
func tableSchema[T any](typ reflect.Type, wide bool) ([]string, func(reflect.Value) []any) {
	// 1. Opt-in override.
	if _, ok := any((*T)(nil)).(TableRower); ok {
		sample := any(reflect.New(typ).Interface()).(TableRower)
		header := sample.TableHeader(wide)
		return header, func(ptr reflect.Value) []any {
			return ptr.Interface().(TableRower).TableRow(wide)
		}
	}

	// 2. Types embedding model.Resource.
	if _, ok := any((*T)(nil)).(resourceMeta); ok {
		cols := reflectColumns(typ, wide)
		header := []string{"NAME"}
		if wide {
			header = append(header, "ID")
		}
		for _, c := range cols {
			header = append(header, c.header)
		}
		header = append(header, "STATUS")
		if wide {
			header = append(header, "PROJECT", "REVISION")
		}
		header = append(header, "AGE")

		return header, func(ptr reflect.Value) []any {
			meta := ptr.Interface().(resourceMeta).Metadata()
			row := []any{meta.Name}
			if wide {
				row = append(row, meta.ID)
			}
			elem := ptr.Elem()
			for _, c := range cols {
				row = append(row, formatValue(elem.FieldByIndex(c.index)))
			}
			row = append(row, meta.Status)
			if wide {
				row = append(row, meta.ProjectID, meta.Revision)
			}
			row = append(row, HumanizeAge(meta.UpdatedAt))
			return row
		}
	}

	// 3. Manifests: the envelope a product's own resources travel in.
	if typ == reflect.TypeOf(manifest.ResourceManifest{}) {
		header := []string{"NAME", "KIND"}
		if wide {
			header = append(header, "UID", "VERSION", "LABELS")
		}
		header = append(header, "AGE")
		return header, func(ptr reflect.Value) []any {
			m := ptr.Interface().(*manifest.ResourceManifest)
			row := []any{m.Metadata.Name, m.Kind}
			if wide {
				row = append(row, m.Metadata.UID, m.Metadata.Version, labelList(m.Metadata.Labels))
			}
			var created time.Time
			if m.Metadata.CreatedAt != nil {
				created = *m.Metadata.CreatedAt
			}
			return append(row, HumanizeAge(created))
		}
	}

	// 4. Generic struct: every scalar top-level field.
	if typ.Kind() != reflect.Struct {
		return nil, nil
	}
	cols := reflectColumns(typ, true)
	if len(cols) == 0 {
		return nil, nil
	}
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.header
	}
	return header, func(ptr reflect.Value) []any {
		elem := ptr.Elem()
		row := make([]any, len(cols))
		for i, c := range cols {
			row[i] = formatValue(elem.FieldByIndex(c.index))
		}
		return row
	}
}

func labelList(labels manifest.Labels) string {
	if len(labels) == 0 {
		return "-"
	}
	pairs := make([]string, 0, len(labels))
	for key, value := range labels {
		pairs = append(pairs, key+"="+value)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

type structField struct {
	header string
	index  []int
}

// reflectColumns enumerates exported scalar fields declared directly on the
// struct, skipping the embedded Resource and any complex/anonymous fields. In
// short mode the number of columns is capped by maxShortColumns.
func reflectColumns(typ reflect.Type, wide bool) []structField {
	if typ.Kind() != reflect.Struct {
		return nil
	}
	var cols []structField
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous || !f.IsExported() {
			continue
		}
		if !isScalarKind(f.Type.Kind()) {
			continue
		}
		if strings.Contains(f.Tag.Get("json"), "-") && jsonName(f) == "" {
			continue
		}
		cols = append(cols, structField{header: columnHeader(f), index: f.Index})
		if !wide && len(cols) >= maxShortColumns {
			break
		}
	}
	return cols
}

func isScalarKind(k reflect.Kind) bool {
	switch k {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// jsonName returns the field's json tag name (before the first comma), if any.
func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return ""
	}
	return name
}

func columnHeader(f reflect.StructField) string {
	name := jsonName(f)
	if name == "" {
		name = f.Name
	}
	return strings.ToUpper(name)
}

// formatValue renders a reflected scalar field, dereferencing pointers.
func formatValue(v reflect.Value) any {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	return v.Interface()
}

// HumanizeAge renders a kubectl-style short duration since t.
func HumanizeAge(t time.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	return ShortDuration(time.Since(t))
}

// ShortDuration formats d in a compact kubectl-style unit (45s, 12m, 3h, 5d).
func ShortDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}
