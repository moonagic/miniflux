// Copyright 2017 Frédéric Guillot. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package scraper // import "miniflux.app/reader/scraper"

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"miniflux.app/config"
	"miniflux.app/http/client"
	"miniflux.app/logger"
	"miniflux.app/reader/readability"
	"miniflux.app/url"

	gurl "net/url"

	"github.com/PuerkitoBio/goquery"
	ra "github.com/go-shiori/go-readability"
)

// Fetch downloads a web page and returns relevant contents.
func Fetch(websiteURL, rules, userAgent string, cookie string, allowSelfSignedCertificates, useProxy bool) (string, error) {
	clt := client.NewClientWithConfig(websiteURL, config.Opts)
	clt.WithUserAgent(userAgent)
	clt.WithCookie(cookie)
	if useProxy {
		clt.WithProxy()
	}
	clt.AllowSelfSignedCertificates = allowSelfSignedCertificates

	response, err := clt.Get()
	if err != nil {
		return "", err
	}

	if response.HasServerFailure() {
		return "", errors.New("scraper: unable to download web page")
	}

	if !isAllowedContentType(response.ContentType) {
		return "", fmt.Errorf("scraper: this resource is not a HTML document (%s)", response.ContentType)
	}

	if err = response.EnsureUnicodeBody(); err != nil {
		return "", err
	}

	// The entry URL could redirect somewhere else.
	websiteURL = response.EffectiveURL

	if rules == "" {
		rules = getPredefinedScraperRules(websiteURL)
	}

	var content string
	if rules == "ra" {
		logger.Debug(`[Scraper] Using rules %q for %q`, rules, websiteURL)
		content, err = readabilityContent(websiteURL)
	} else if rules != "" {
		logger.Debug(`[Scraper] Using rules %q for %q`, rules, websiteURL)
		content, err = scrapContent(response.Body, rules)
	} else {
		logger.Debug(`[Scraper] Using readability for %q`, websiteURL)
		content, err = readability.ExtractContent(response.Body)
	}

	if err != nil {
		return "", err
	}

	return content, nil
}

func scrapContent(page io.Reader, rules string) (string, error) {
	document, err := goquery.NewDocumentFromReader(page)
	if err != nil {
		return "", err
	}

	contents := ""
	document.Find(rules).Each(func(i int, s *goquery.Selection) {
		var content string

		content, _ = goquery.OuterHtml(s)
		contents += content
	})

	return contents, nil
}

func readabilityContent(website string) (string, error) {
	parsedURL, err := gurl.ParseRequestURI(website)
	if err != nil {
		return "", fmt.Errorf("scraper: invalid url")
	}
	// Fetch page from URL
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Get(website)
	if err != nil {
		return "", fmt.Errorf("scraper: invalid url")
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	parser := ra.NewParser()
	article, err := parser.Parse(resp.Body, parsedURL)
	if err != nil {
		return "", fmt.Errorf("scraper: invalid url")
	}
	return article.Content, nil
}

func getPredefinedScraperRules(websiteURL string) string {
	urlDomain := url.Domain(websiteURL)

	for domain, rules := range predefinedRules {
		if strings.Contains(urlDomain, domain) {
			return rules
		}
	}

	return ""
}

func isAllowedContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.HasPrefix(contentType, "text/html") ||
		strings.HasPrefix(contentType, "application/xhtml+xml")
}
