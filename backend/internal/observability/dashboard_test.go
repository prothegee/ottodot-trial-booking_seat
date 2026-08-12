package observability_test

import (
    "encoding/json"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
    "testing"
)

/*
The dashboards and the alert rules are files nobody compiles.

A metric name in a Grafana panel is a string, and a metric name in an alert rule
is a string, and neither of them fails when the code stops publishing it. The
panel goes blank and the alert goes quiet, and both of those look exactly like a
healthy service.

These cases are the compiler those two files do not have. They read the json and
the yaml, pull out every metric name, and check it against what this process
actually exposes.
*/

// dashboardDirectory and ruleFile are where the provisioned files live.
//
// They are relative paths out of this package, which is the one thing that would
// break if either moved. That is deliberate: a moved dashboard should fail here
// rather than be silently unprovisioned.
const (
    dashboardDirectory = "../../containers/grafana/dashboards"
    ruleFile           = "../../containers/prometheus/rules.yml"
)

// metricNamePattern is a bare identifier in a query.
//
// Everything it catches is filtered against the keyword list below, so a false
// match becomes a name nobody recognises rather than a wrong pass.
var metricNamePattern = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)

// labelMatcherPattern is a label selector, braces and all.
//
// It is stripped before the metric names are read, because a label value is
// arbitrary text and reads exactly like a metric name. `{code=~"class_full"}`
// would otherwise be reported as a missing metric called class_full.
var labelMatcherPattern = regexp.MustCompile(`\{[^}]*\}`)

// promqlKeywords are the words in a query that are not metric names.
var promqlKeywords = map[string]bool{
    "sum": true, "rate": true, "increase": true, "avg": true, "min": true, "max": true,
    "by": true, "without": true, "histogram_quantile": true, "changes": true, "count": true,
    "le": true, "and": true, "or": true, "unless": true, "on": true, "ignoring": true,
    "group_left": true, "group_right": true, "job": true, "name": true, "mode": true,
    "mountpoint": true, "fstype": true, "device": true, "outcome": true, "step": true,
    "code": true, "reason": true, "scope": true, "check": true, "kind": true, "state": true,
    "route": true, "result": true, "pool": true, "point": true, "component": true,
    "api": true, "worker": true, "tmpfs": true, "overlay": true, "idle": true, "up": true,
    "irate": true, "delta": true, "clamp_min": true, "clamp_max": true, "topk": true,
    "alertname": true, "severity": true, "stack": true, "endpoint": true,
}

// externalMetricPrefixes are the metrics this service does not publish and must
// not be expected to.
//
// Layer two and layer three of the monitoring plan come from node_exporter and
// cAdvisor, and layer one's runtime numbers come from the Prometheus client
// library's own collectors. None of those names are in this project's catalogue,
// and a test that demanded they be would be wrong rather than strict.
var externalMetricPrefixes = []string{
    "node_",
    "container_",
    "process_",
    "go_",
    "promhttp_",
    "scrape_",
}

// published is every metric name this process actually exposes.
//
// It reads a registry that has been driven rather than one that has merely been
// built, so a name only counts as published when a code path exists to produce
// it. A list taken from the constants would pass even if half of them were never
// registered, which is exactly the failure this file exists to catch.
func published(t *testing.T) map[string]bool {
    t.Helper()

    families, err := drivenRegistry(t).Gather()
    if err != nil {
        t.Fatalf("the registry could not be gathered: %v", err)
    }

    names := make(map[string]bool, len(families))

    for _, family := range families {
        name := family.GetName()

        names[name] = true

        // A histogram publishes three derived names, and a dashboard queries the
        // bucket one by hand. They are real series and they belong here.
        names[name+"_bucket"] = true
        names[name+"_sum"] = true
        names[name+"_count"] = true
    }

    return names
}

// isExternal reports whether a name belongs to a collector outside this project.
func isExternal(name string) bool {
    for _, prefix := range externalMetricPrefixes {
        if strings.HasPrefix(name, prefix) {
            return true
        }
    }

    return false
}

// metricsIn pulls every plausible metric name out of one query.
func metricsIn(query string) []string {
    var found []string

    for _, word := range metricNamePattern.FindAllString(labelMatcherPattern.ReplaceAllString(query, " "), -1) {
        if promqlKeywords[word] || !strings.Contains(word, "_") {
            continue
        }

        found = append(found, word)
    }

    return found
}

// dashboardQueries reads every panel expression out of one dashboard file.
func dashboardQueries(t *testing.T, path string) []string {
    t.Helper()

    raw, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("the dashboard could not be read: %v", err)
    }

    var dashboard struct {
        Panels []struct {
            Title   string `json:"title"`
            Targets []struct {
                Expr string `json:"expr"`
            } `json:"targets"`
        } `json:"panels"`
    }

    if err := json.Unmarshal(raw, &dashboard); err != nil {
        t.Fatalf("the dashboard is not valid json, so Grafana would refuse it: %v", err)
    }

    var queries []string

    for _, panel := range dashboard.Panels {
        for _, target := range panel.Targets {
            if strings.TrimSpace(target.Expr) != "" {
                queries = append(queries, target.Expr)
            }
        }
    }

    return queries
}

func TestTheDashboardsQueryMetricsThatExist(t *testing.T) {
    t.Run("integration: every panel names a metric something publishes", func(t *testing.T) {
        // The failure this catches is a rename. A metric renamed in code and not
        // in the dashboard turns a panel blank, and a blank panel on a quiet
        // afternoon looks exactly like a healthy service.
        exposed := published(t)

        files, err := filepath.Glob(filepath.Join(dashboardDirectory, "*.json"))
        if err != nil || len(files) == 0 {
            t.Fatalf("no dashboards were found in %s: %v", dashboardDirectory, err)
        }

        sort.Strings(files)

        for _, path := range files {
            for _, query := range dashboardQueries(t, path) {
                for _, name := range metricsIn(query) {
                    if isExternal(name) || exposed[name] {
                        continue
                    }

                    t.Errorf("%s queries %q, which nothing publishes", filepath.Base(path), name)
                }
            }
        }
    })

    t.Run("edge: the transaction failure panel names the exact series its alert fires on", func(t *testing.T) {
        // The panel and the alert have to agree by string, because nothing else
        // holds them together. If the panel drew a different outcome from the one
        // the alert watches, somebody would look at a flat line while the alert
        // was firing.
        wanted := `confirm_transaction_total{outcome="error"}`

        found := false

        for _, query := range dashboardQueries(t, filepath.Join(dashboardDirectory, "backend.json")) {
            if strings.Contains(query, wanted) {
                found = true
            }
        }

        if !found {
            t.Fatalf("no panel on the backend dashboard queries %s", wanted)
        }
    })

    t.Run("behaviour: cpu, memory, and drive resolve without cAdvisor", func(t *testing.T) {
        // cAdvisor reads a container runtime socket and is written against
        // Docker, so under rootless Podman it may simply not start. When it
        // does not, the dashboard has to degrade from per container to host
        // wide rather than lose cpu, memory, and drive entirely.
        queries := dashboardQueries(t, filepath.Join(dashboardDirectory, "backend.json"))

        // Layer one is the process collectors, layer two is node_exporter, and
        // neither needs a container runtime socket.
        withoutCadvisor := make([]string, 0, len(queries))

        for _, query := range queries {
            if !strings.Contains(query, "container_") {
                withoutCadvisor = append(withoutCadvisor, query)
            }
        }

        joined := strings.Join(withoutCadvisor, "\n")

        for what, series := range map[string]string{
            "cpu":    "node_cpu_seconds_total",
            "memory": "node_memory_MemAvailable_bytes",
            "drive":  "node_filesystem_avail_bytes",
        } {
            if !strings.Contains(joined, series) {
                t.Errorf("with cAdvisor absent there is no %s panel, because none of the remaining queries names %s", what, series)
            }
        }

        for what, series := range map[string]string{
            "process cpu":    "process_cpu_seconds_total",
            "process memory": "process_resident_memory_bytes",
        } {
            if !strings.Contains(joined, series) {
                t.Errorf("with cAdvisor absent there is no %s panel, because none of the remaining queries names %s", what, series)
            }
        }
    })

    t.Run("unit: every dashboard reads the provisioned data source", func(t *testing.T) {
        // A panel pointed at a data source uid nobody provisions draws nothing
        // and says nothing about why.
        files, _ := filepath.Glob(filepath.Join(dashboardDirectory, "*.json"))

        for _, path := range files {
            raw, err := os.ReadFile(path)
            if err != nil {
                t.Fatalf("the dashboard could not be read: %v", err)
            }

            if !strings.Contains(string(raw), "ottodot-prometheus") {
                t.Errorf("%s does not name the provisioned data source", filepath.Base(path))
            }
        }
    })
}

func TestTheAlertRulesQueryMetricsThatExist(t *testing.T) {
    t.Run("integration: every rule names a metric something publishes", func(t *testing.T) {
        exposed := published(t)

        raw, err := os.ReadFile(ruleFile)
        if err != nil {
            t.Fatalf("the rule file could not be read: %v", err)
        }

        // The expressions are pulled by line rather than by parsing yaml, which
        // keeps this package free of a yaml dependency for one file. Every rule
        // in the file is written as a single line `expr:`, and a rule that is
        // not would simply not be checked, which the count assertion below is
        // what catches.
        var expressions []string

        for _, line := range strings.Split(string(raw), "\n") {
            trimmed := strings.TrimSpace(line)

            if after, found := strings.CutPrefix(trimmed, "expr:"); found {
                expressions = append(expressions, after)
            }
        }

        if len(expressions) < 13 {
            t.Fatalf("only %d rule expressions were found, and the plan calls for thirteen alerts", len(expressions))
        }

        for _, expression := range expressions {
            for _, name := range metricsIn(expression) {
                if isExternal(name) || exposed[name] {
                    continue
                }

                t.Errorf("an alert rule queries %q, which nothing publishes", name)
            }
        }
    })

    t.Run("edge: every alert the plan names is present", func(t *testing.T) {
        // An alert quietly dropped in an edit is coverage quietly lost, and
        // nothing else in the repository would notice.
        raw, err := os.ReadFile(ruleFile)
        if err != nil {
            t.Fatalf("the rule file could not be read: %v", err)
        }

        for _, alert := range []string{
            "RefundBacklog",
            "TransactionErrorSpike",
            "AccessDeniedSpike",
            "RefreshReuse",
            "DriveFilling",
            "MemoryPressure",
            "CpuSaturated",
            "ContainerRestarting",
            "QueueStalled",
            "RaceLostSpike",
            "NotReady",
            "ReplicationLag",
        } {
            if !strings.Contains(string(raw), "alert: "+alert) {
                t.Errorf("the rule file has no alert named %s", alert)
            }
        }
    })
}
