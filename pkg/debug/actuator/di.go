package actuator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/samber/do/v2"
)

// renderDIGraph renders the injector's service dependency graph as Mermaid or
// DOT. do/v2 has no graph export, so we walk ExplainInjector for the service
// set and ExplainNamedService per service for its edges.
func renderDIGraph(injector do.Injector, format string) string {
	names := collectServiceNames(injector)

	type edge struct{ from, to string }
	var edges []edge
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		seen[n] = true
	}

	for _, n := range names {
		svc, ok := do.ExplainNamedService(injector, n)
		if !ok {
			continue
		}
		for _, dep := range svc.Dependencies {
			if seen[dep.Service] {
				edges = append(edges, edge{from: n, to: dep.Service})
			}
		}
	}

	if format == "dot" {
		var b strings.Builder
		b.WriteString("digraph di {\n")
		for _, n := range names {
			fmt.Fprintf(&b, "  %q;\n", n)
		}
		for _, e := range edges {
			fmt.Fprintf(&b, "  %q -> %q;\n", e.from, e.to)
		}
		b.WriteString("}\n")
		return b.String()
	}

	var b strings.Builder
	b.WriteString("graph LR\n")
	ids := make(map[string]string, len(names))
	for i, n := range names {
		id := fmt.Sprintf("n%d", i)
		ids[n] = id
		fmt.Fprintf(&b, "  %s[%q]\n", id, n)
	}
	for _, e := range edges {
		fmt.Fprintf(&b, "  %s --> %s\n", ids[e.from], ids[e.to])
	}
	return b.String()
}

// collectServiceNames returns the sorted, de-duplicated set of service names
// across every scope in the injector's DAG.
func collectServiceNames(injector do.Injector) []string {
	out := do.ExplainInjector(injector)
	set := make(map[string]struct{})

	var walk func(scopes []do.ExplainInjectorScopeOutput)
	walk = func(scopes []do.ExplainInjectorScopeOutput) {
		for _, s := range scopes {
			for _, svc := range s.Services {
				set[svc.ServiceName] = struct{}{}
			}
			walk(s.Children)
		}
	}
	walk(out.DAG)

	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
