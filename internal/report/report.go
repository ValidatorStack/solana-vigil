// Package report renders operator snapshots as text, JSON, or self-contained HTML.
package report

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/audit"
	"github.com/ValidatorStack/solana-vigil/internal/buildinfo"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

type Document struct {
	Snapshot monitor.Snapshot `json:"snapshot"`
	Audit    *audit.Report    `json:"audit,omitempty"`
}

func WriteJSON(writer io.Writer, document Document) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}

func WriteText(writer io.Writer, document Document) {
	snapshot := document.Snapshot
	fmt.Fprintf(writer, "Vigil validator check — %s\n", snapshot.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(writer, "Overall: %s\n", strings.ToUpper(string(snapshot.OverallStatus())))
	fmt.Fprintf(writer, "Vote: %s\nIdentity: %s\nClient: %s\n", snapshot.VoteAccount, emptyUnknown(snapshot.Identity), emptyUnknown(snapshot.ClientVersion))
	if snapshot.VAT.RunwayEpochs != nil {
		fmt.Fprintf(writer, "VAT runway: %.3f epochs | next epoch sufficient: %t\n", *snapshot.VAT.RunwayEpochs, snapshot.VAT.SufficientNextEpoch != nil && *snapshot.VAT.SufficientNextEpoch)
	}
	fmt.Fprintf(writer, "BLS: %s\n", emptyUnknown(snapshot.BLSState))
	if snapshot.Leader.Available {
		fmt.Fprintf(writer, "Leader production: %d/%d produced | missed: %d | skip: %.3f%%\n", snapshot.Leader.BlocksProduced, snapshot.Leader.LeaderSlots, snapshot.Leader.MissedSlots, snapshot.Leader.SkipRatePercent)
	}
	fmt.Fprintln(writer)
	for _, check := range snapshot.Checks {
		fmt.Fprintf(writer, "%-8s %-28s %s\n", strings.ToUpper(string(check.Status)), check.ID, check.Summary)
		if check.Detail != "" && check.Status != monitor.OK {
			fmt.Fprintf(writer, "         %s\n", check.Detail)
		}
	}
	if document.Audit != nil {
		fmt.Fprintln(writer)
		audit.WriteText(writer, *document.Audit)
	}
}

func WriteHTML(writer io.Writer, document Document) error {
	data := struct {
		Document
		Overall string
		Version string
		SOL     func(*uint64) string
	}{Document: document, Overall: string(document.Snapshot.OverallStatus()), Version: buildinfo.String(), SOL: func(value *uint64) string {
		if value == nil {
			return "unknown"
		}
		return fmt.Sprintf("%.9f", float64(*value)/1e9)
	}}
	engine, err := template.New("report").Funcs(template.FuncMap{
		"deref": func(value *float64) float64 {
			if value == nil {
				return 0
			}
			return *value
		},
	}).Parse(htmlTemplate)
	if err != nil {
		return err
	}
	return engine.Execute(writer, data)
}

func emptyUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

const htmlTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Vigil validator report</title>
<style>
:root{color-scheme:dark;--bg:#0b1010;--panel:#121a19;--text:#e8f0ed;--muted:#9aaba5;--ok:#4ade80;--warning:#fbbf24;--critical:#fb7185;--unknown:#a5b4fc}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:15px/1.55 ui-monospace,SFMono-Regular,Consolas,monospace}main{max-width:1100px;margin:auto;padding:40px 22px}header{display:flex;justify-content:space-between;gap:20px;align-items:start;border-bottom:1px solid #293633;margin-bottom:24px}h1{font-size:28px;margin:0}h2{font-size:18px;margin-top:32px}.muted,small{color:var(--muted)}.badge{display:inline-block;padding:5px 10px;border:1px solid currentColor;border-radius:4px;text-transform:uppercase;font-weight:700}.ok{color:var(--ok)}.warning{color:var(--warning)}.critical{color:var(--critical)}.unknown{color:var(--unknown)}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:12px}.card,section{background:var(--panel);border:1px solid #26322f;border-radius:8px;padding:16px}.card b{display:block;font-size:20px;margin-top:4px;overflow-wrap:anywhere}section{padding:0;overflow:hidden}table{border-collapse:collapse;width:100%}th,td{text-align:left;vertical-align:top;padding:12px 14px;border-bottom:1px solid #26322f}th{color:var(--muted);font-size:12px;text-transform:uppercase}td:last-child{width:58%;overflow-wrap:anywhere}footer{margin-top:28px;color:var(--muted);font-size:12px}@media(max-width:650px){th:nth-child(1),td:nth-child(1){display:none}td:last-child{width:auto}}
</style></head><body><main>
<header><div><h1>Vigil validator report</h1><div class="muted">{{.Snapshot.Timestamp.Format "2006-01-02 15:04:05 UTC"}}</div></div><span class="badge {{.Overall}}">{{.Overall}}</span></header>
<div class="grid">
<div class="card"><small>Vote account</small><b>{{.Snapshot.VoteAccount}}</b></div>
<div class="card"><small>Identity</small><b>{{.Snapshot.Identity}}</b></div>
<div class="card"><small>Client</small><b>{{.Snapshot.ClientVersion}}</b></div>
<div class="card"><small>VAT runway</small><b>{{if .Snapshot.VAT.RunwayEpochs}}{{printf "%.3f epochs" (deref .Snapshot.VAT.RunwayEpochs)}}{{else}}unknown{{end}}</b></div>
<div class="card"><small>BLS</small><b>{{.Snapshot.BLSState}}</b></div>
<div class="card"><small>Leader skip</small><b>{{printf "%.3f%%" .Snapshot.Leader.SkipRatePercent}}</b></div>
</div>
<h2>Validator checks</h2><section><table><thead><tr><th>Status</th><th>Check</th><th>Evidence</th></tr></thead><tbody>
{{range .Snapshot.Checks}}<tr><td class="{{.Status}}">{{.Status}}</td><td>{{.ID}}<br><small>{{.Summary}}</small></td><td>{{.Detail}}</td></tr>{{end}}
</tbody></table></section>
{{if .Audit}}<h2>Read-only Linux audit</h2><section><table><thead><tr><th>Status</th><th>Rule</th><th>Observed / recommendation</th></tr></thead><tbody>{{range .Audit.Findings}}<tr><td class="{{.Status}}">{{.Status}}</td><td>{{.ID}}<br><small>{{.Title}}</small></td><td>{{.Observed}}<br><small>{{.Recommendation}}</small></td></tr>{{end}}</tbody></table></section>{{end}}
<footer>{{.Version}} · Read-only observation; no keys, transactions, host changes, or failover. Protocol fees and thresholds must be verified against your cluster and governance.</footer>
</main></body></html>`
