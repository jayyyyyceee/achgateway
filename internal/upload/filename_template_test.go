// Copyright 2020 The Moov Authors
// Use of this source code is governed by an Apache License
// license that can be found in the LICENSE file.

package upload

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/moov-io/achgateway/internal/service"

	"github.com/stretchr/testify/require"
)

func TestConfig__OutboundFilenameTemplate(t *testing.T) {
	var cfg *service.Shard
	if tmpl := cfg.FilenameTemplate(); tmpl != service.DefaultFilenameTemplate {
		t.Errorf("expected default template: %v", tmpl)
	}

	cfg = &service.Shard{
		OutboundFilenameTemplate: `{{ date "20060102" }}`,
	}
	if tmpl := cfg.FilenameTemplate(); tmpl == service.DefaultFilenameTemplate {
		t.Errorf("expected custom template: %v", tmpl)
	}
}

func TestFilenameTemplate(t *testing.T) {
	// default
	filename, err := RenderACHFilename(service.DefaultFilenameTemplate, FilenameData{
		RoutingNumber: "987654320",
		GPG:           true,
	})
	require.NoError(t, err)

	now := time.Now()
	yymmdd := now.Format("20060102")
	hhmm := now.Format("150405")
	expected := fmt.Sprintf("%s-%s-987654320-0.ach.gpg", yymmdd, hhmm)
	if filename != expected {
		t.Errorf("filename=%s", filename)
	}

	// example from original issue
	linden := `{{ date "20060102" }}.ach`
	filename, err = RenderACHFilename(linden, FilenameData{
		// not included in template
		GPG:           true,
		RoutingNumber: "987654320",
	})
	require.NoError(t, err)

	expected = time.Now().Format("20060102") + ".ach"
	if filename != expected {
		t.Errorf("filename=%s", filename)
	}
}

func TestFilenameTemplate__functions(t *testing.T) {
	cases := []struct {
		tmpl, expected string
		data           FilenameData
	}{
		{
			tmpl:     "static-template",
			expected: "static-template",
		},
		{
			tmpl:     `{{ env "PATH" }}`,
			expected: os.Getenv("PATH"),
		},
		{
			tmpl:     `{{ date "2006-01-02" }}`,
			expected: time.Now().Format("2006-01-02"),
		},
		{
			tmpl:     `foo-{{ upper .ShardName }}-{{ .Index }}.ach`,
			expected: "foo-LIVE-1.ach",
			data: FilenameData{
				ShardName: "live",
				Index:     1,
			},
		},
	}
	for i := range cases {
		res, err := RenderACHFilename(cases[i].tmpl, cases[i].data)
		if err != nil {
			t.Errorf("#%d: %v", i, err)
		}
		if cases[i].expected != res {
			t.Errorf("#%d: %s", i, res)
		}
	}
}
