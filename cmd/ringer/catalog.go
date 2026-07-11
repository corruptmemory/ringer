// cmd/ringer/catalog.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/corruptmemory/ringer/internal/catalog"
	"github.com/corruptmemory/ringer/internal/store"
)

type catalogCmd struct {
	Refresh bool   `long:"refresh"`
	Source  string `long:"source" description:"fetch source URL or file (default OpenRouter)"`
	File    string `long:"file" description:"read a catalog JSON file instead of the DB"`
	Free    bool   `long:"free" description:"only free models"`
	Changes bool   `long:"changes" description:"recent catalog change events"`
	JSON    bool   `long:"json"`
}

func (c *catalogCmd) Execute(args []string) error {
	// A stray positional is a user error (a mistyped flag, a shell-glob that
	// expanded) — fail loud rather than silently ignore it (this project's
	// no-silent-failures rule), matching modelsCmd.Execute's guard.
	if len(args) > 0 {
		return fmt.Errorf("catalog: unexpected argument %q", args[0])
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer s.Close()

	if c.Refresh {
		src := c.Source
		if src == "" {
			src = catalog.DefaultSource
		}
		if _, err := catalog.Refresh(s, src, catalog.FetchTimeout, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if c.Changes {
		evs, err := s.CatalogEvents(20)
		if err != nil {
			return err
		}
		for _, e := range evs {
			fmt.Println(describeCatalogEvent(e))
		}
		return nil
	}

	var models []catalog.Model
	switch {
	case c.File != "":
		models, err = catalog.LoadModelsFile(c.File)
	case c.Free:
		models, err = s.FreeCatalogModels()
	default:
		models, err = s.CatalogModels()
	}
	if err != nil {
		return err
	}
	if c.Free && c.File != "" {
		models = filterFree(models)
	}
	if c.JSON {
		return json.NewEncoder(os.Stdout).Encode(models)
	}
	if len(models) == 0 {
		if c.File != "" {
			return fmt.Errorf("no catalog models in %s", c.File)
		}
		return fmt.Errorf("no catalog models; run 'ringer catalog --refresh'")
	}
	renderCatalogTable(os.Stdout, models)
	return nil
}

func filterFree(models []catalog.Model) []catalog.Model {
	var out []catalog.Model
	for _, m := range models {
		if m.Free {
			out = append(out, m)
		}
	}
	return out
}

func renderCatalogTable(w io.Writer, models []catalog.Model) {
	catalog.SortModels(models)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "id\t$/M in\t$/M out\tctx\tFREE")
	for _, m := range models {
		marker := ""
		if m.Free {
			marker = "FREE"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", m.ID, price(m.PromptPerM, m.VariablePricing), price(m.CompletionPerM, m.VariablePricing), m.ContextLength, marker)
	}
	tw.Flush()
}

func price(v *float64, variable bool) string {
	if variable || v == nil {
		return "var"
	}
	if *v == 0 {
		return "0"
	}
	return fmt.Sprintf("%.4g", *v)
}

// describeCatalogEvent ports ringer.py:1597-1624 (Go-authoritative wording).
func describeCatalogEvent(e catalog.Event) string {
	switch e.Kind {
	case "price_change":
		return fmt.Sprintf("%s %s price_change: in %v->%v, out %v->%v", e.TS, e.ModelID,
			e.Payload["old_prompt_per_m"], e.Payload["new_prompt_per_m"], e.Payload["old_completion_per_m"], e.Payload["new_completion_per_m"])
	case "went_free", "went_paid":
		return fmt.Sprintf("%s %s %s", e.TS, e.ModelID, e.Kind)
	case "added":
		marker := ""
		if free, _ := e.Payload["free"].(bool); free {
			marker = " FREE"
		}
		return fmt.Sprintf("%s %s added%s", e.TS, e.ModelID, marker)
	case "removed":
		return fmt.Sprintf("%s %s removed", e.TS, e.ModelID)
	default:
		return fmt.Sprintf("%s %s %s", e.TS, e.ModelID, e.Kind)
	}
}

func init() {
	parser.AddCommand("catalog", "OpenRouter model catalog",
		"Show or refresh the local OpenRouter model catalog (stored in SQLite).", &catalogCmd{})
}
