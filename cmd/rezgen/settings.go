package main

import (
	"os"

	"github.com/Blathe/rezgen/internal/config"
	"github.com/Blathe/rezgen/internal/llm"
)

// defaults fills in flags the user left empty from the settings saved by the
// app's first-run setup, falling back to the paths rezgen used before setup
// existed (./profile.json and ./applications). A saved API key is used when
// none is in the environment. Pass nil for values a command doesn't take.
func defaults(profilePath, outDir, model *string) {
	conf, err := config.Load()
	ok := err == nil
	set := func(p *string, fromConf func() string, fallback string) {
		if p == nil || *p != "" {
			return
		}
		if ok && fromConf() != "" {
			*p = fromConf()
		} else {
			*p = fallback
		}
	}
	set(profilePath, func() string { return conf.ProfilePath() }, "profile.json")
	set(outDir, func() string { return conf.ApplicationsDir() }, "applications")
	set(model, func() string { return conf.Model }, llm.DefaultModel)
	if ok && conf.APIKey != "" && os.Getenv("ANTHROPIC_API_KEY") == "" && conf.Provider == config.Anthropic {
		os.Setenv("ANTHROPIC_API_KEY", conf.APIKey)
	}
}
