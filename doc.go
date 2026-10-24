// SPDX-License-Identifier: Apache-2.0

// Package cistatus reports the current state of chosen GitHub Actions
// workflows on chosen branches: what is red, since when, where it fails, and
// when it was last green.
//
// It is the library behind the ci-status command and its local web page, and
// is meant for other programs too, such as an MCP server. It only reads
// GitHub, with a token from the GitHub CLI (GHToken) or any other source.
//
// A typical use:
//
//	token, err := cistatus.GHToken(ctx)
//	if err != nil {
//		return err
//	}
//	client := cistatus.NewClient(token, cistatus.DefaultAPIURL)
//	dir, _ := cistatus.ConfigDir()
//	cfg, _, err := cistatus.LoadConfig(dir)
//	if err != nil {
//		return err
//	}
//	var rows []cistatus.Row
//	for _, rc := range cfg.Repos {
//		rows = append(rows, cistatus.Collect(ctx, client, rc.Repo, rc.Watch, time.Now())...)
//	}
//	report := cistatus.NewReport(cfg.RepoNames(), cistatus.SortRows(rows), time.Now())
//
// The setup checks (CheckGH, CheckLogin, CheckAPI, CheckRepo) explain what is
// missing in words a person can act on. Config's methods add and remove
// repositories and watches without changing the Config they are called on.
package cistatus
