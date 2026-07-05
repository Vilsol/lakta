package lakta

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Vilsol/slox"
)

// envDebugWiring, when set to "1", forces the boot-time wiring report to stdout
// as an aligned table in addition to the always-on debug-level log line.
const envDebugWiring = "LAKTA_DEBUG_WIRING"

// RenderWiringReport renders a RuntimeInfo snapshot as an aligned text table
// with columns order, module, lifecycle, provides, consumes, and init duration.
// When prov is non-empty, a config-provenance section (key -> origin) is
// appended. Used by the boot-time debug log and the LAKTA_DEBUG_WIRING=1 dump.
func RenderWiringReport(info []ModuleInfo, prov map[string]string) string {
	headers := []string{"ORDER", "MODULE", "LIFECYCLE", "PROVIDES", "CONSUMES", "INIT"}
	rows := make([][]string, 0, len(info))

	for _, m := range info {
		module := m.Type
		if m.Name != "" {
			module = m.Type + " (" + m.Name + ")"
		}

		rows = append(rows, []string{
			strconv.Itoa(m.InitOrder),
			module,
			m.Lifecycle.String(),
			joinOrDash(m.Provides),
			joinOrDash(consumes(m)),
			m.InitDuration.String(),
		})
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var b strings.Builder
	writeRow(&b, headers, widths)
	for _, row := range rows {
		writeRow(&b, row, widths)
	}

	if len(prov) > 0 {
		b.WriteString("\nconfig provenance:\n")
		keys := make([]string, 0, len(prov))
		for k := range prov {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s = %s\n", k, prov[k])
		}
	}

	return b.String()
}

func writeRow(b *strings.Builder, cells []string, widths []int) {
	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(cell)
		if i < len(cells)-1 {
			b.WriteString(strings.Repeat(" ", widths[i]-len(cell)))
		}
	}
	b.WriteString("\n")
}

func consumes(m ModuleInfo) []string {
	out := make([]string, 0, len(m.Requires)+len(m.Optional))
	out = append(out, m.Requires...)
	out = append(out, m.Optional...)
	return out
}

func joinOrDash(s []string) string {
	if len(s) == 0 {
		return "-"
	}
	return strings.Join(s, ", ")
}

// emitWiringReport logs the wiring report at debug level always, and dumps it to
// stdout when LAKTA_DEBUG_WIRING=1. Called after init so durations are recorded.
func emitWiringReport(ctx context.Context, info *RuntimeInfo) {
	report := RenderWiringReport(info.Snapshot(), nil)

	slox.Debug(ctx, "module wiring report\n"+report)

	if os.Getenv(envDebugWiring) == "1" {
		_, _ = fmt.Fprint(os.Stdout, report)
	}
}
