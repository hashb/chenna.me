package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"
)

type jekyllFrontMatter struct {
	Layout    string   `yaml:"layout"`
	Date      string   `yaml:"date,omitempty"`
	Tags      []string `yaml:"tags,omitempty"`
	Published *bool    `yaml:"published,omitempty"`
}

func buildFrontMatter(date time.Time, tags []string, published bool) (string, error) {
	return marshalFrontMatter(jekyllFrontMatter{
		Layout:    "micro",
		Date:      date.Format("2006-01-02 15:04:05 -0700"),
		Tags:      tags,
		Published: publishedFrontMatterValue(published),
	})
}

func publishedFrontMatterValue(published bool) *bool {
	if published {
		return nil
	}

	publishedValue := false
	return &publishedValue
}

func parseFrontMatter(data string) (jekyllFrontMatter, string, error) {
	var fm jekyllFrontMatter

	content, err := frontmatter.Parse(strings.NewReader(data), &fm)
	if err != nil {
		return jekyllFrontMatter{}, "", fmt.Errorf("parse front matter: %w", err)
	}

	return fm, string(content), nil
}

func rebuildPost(fm jekyllFrontMatter, content string) (string, error) {
	frontMatter, err := marshalFrontMatter(fm)
	if err != nil {
		return "", err
	}

	return frontMatter + "\n" + content, nil
}

func marshalFrontMatter(fm jekyllFrontMatter) (string, error) {
	if fm.Layout == "" {
		fm.Layout = "micro"
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(fm); err != nil {
		return "", fmt.Errorf("marshal front matter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("close front matter encoder: %w", err)
	}

	return fmt.Sprintf("---\n%s---\n", buf.String()), nil
}
