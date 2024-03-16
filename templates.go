package main

import (
	"embed"
	"html/template"
	"strings"
)

//go:embed assets/*
var assets embed.FS

var tmpl = template.Must(template.New("").Funcs(
	template.FuncMap{
		"mod":            mod,
		"replaceNewline": replaceNewline,
	}).ParseFS(assets, "assets/*"))

func mod(a, b int) int {
	return a % b
}

func replaceNewline(s string) template.HTML {
	return template.HTML(strings.Replace(template.HTMLEscapeString(s), "\n", "<br>", -1))
}
