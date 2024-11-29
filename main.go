package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var url string = "https://www.quicksprout.com/sitemap.xml"

type SeoData struct {
	URL             string
	Title           string
	H1              string
	MetaDescription string
	StatusCode      int
}

// There was no need to make this as we can directly implement GetSeoData but now we have written so it will show us how interface works in GoLang
type Parser interface {
	GetSeoData(resp *http.Response) (SeoData, error)
}

// No implementation here as we need to use DefaultParser only 
type DefaultParser struct {
}

// GetSeoData concrete implementation of the default parser & here (d DefaultParser is written because we need to attach this function with Parser Interface)
func (d DefaultParser) GetSeoData(resp *http.Response) (SeoData, error) {
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return SeoData{}, err
	}
	result := SeoData{}
	result.URL = resp.Request.URL.String()
	result.StatusCode = resp.StatusCode
	result.Title = doc.Find("title").First().Text()
	result.H1 = doc.Find("h1").First().Text()
	result.MetaDescription, _ = doc.Find("meta[name^=description]").Attr("content")
	return result, nil
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/61.0.3163.100 Safari/537.36",
	"Mozilla/5.0 (Windows NT 6.1; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/61.0.3163.100 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_12_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/61.0.3163.100 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_12_6) AppleWebKit/604.1.38 (KHTML, like Gecko) Version/11.0 Safari/604.1.38",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:56.0) Gecko/20100101 Firefox/56.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13) AppleWebKit/604.1.38 (KHTML, like Gecko) Version/11.0 Safari/604.1.38",
}

func randomUserAgent() string {
	rand.Seed(time.Now().Unix())
	randNum := rand.Int() % len(userAgents)
	return userAgents[randNum]
}

func extractSitemapURLs(startURL string) []string {
	worklist := make(chan []string) // Channel for batched URLs
	toCrawl := []string{}
	var n int

	// Send the starting URL into the channel
	n++
	go func() { worklist <- []string{startURL} }()

	for n > 0 {
		// Wait for a batch of URLs
		list := <-worklist
		n--

		// Process each URL in the batch
		for _, link := range list {
			if link != "" {
				n++ // Increment for the new goroutine
				go func(link string) {
					defer func() { n-- }() // Decrement once the goroutine finishes
					
					response, err := makeRequest(link)
					if err != nil {
						log.Printf("Request failed: %s", link)
						return
					}

					urls, err := extractUrls(response)
					if err != nil {
						log.Printf("Error extracting URLs: %s", link)
						return
					}

					sitemapFiles, pages := isSitemap(urls)
					if len(sitemapFiles) > 0 {
						worklist <- sitemapFiles // Add sitemap files for further crawling
					}

					toCrawl = append(toCrawl, pages...)
				}(link)
			}
		}
	}

	return toCrawl
}


func makeRequest(url string) (*http.Response, error) {
	// A new HTTP request is tailored with client timeout, type=GET and with User-agent(browser)
	client := http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", randomUserAgent())
	if err != nil {
		return nil, err
	}
	// The new HTTP request is acutally sent 
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func extractUrls(response *http.Response) ([]string, error) {
	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {	return nil, err	}
	results := []string{}

	// for html website we need "a" and extract heref from the text
	sel := doc.Find("loc")				
	for i := range sel.Nodes {
		loc := sel.Eq(i)					//select only node i.e. in ith position
		result := loc.Text()
		results = append(results, result)
	}

	return results, nil
}

// Segregating pages and sitemapfiles(XML thing it is the url containing more URLs and is not a actual page)
func isSitemap(urls []string) ([]string, []string) {
	sitemapFiles := []string{}
	pages := []string{}
	for _, page := range urls {
		foundSitemap := strings.Contains(page, "xml")
		if foundSitemap == true {
			fmt.Println("Found Sitemap", page)
			sitemapFiles = append(sitemapFiles, page)
		} else{
			pages = append(pages, page)
		}
	}
	return sitemapFiles, pages
}

// Parser is used for extracting SEO data from the HTML responses
// It is done just as written so no need to think and waste time again
func scrapeUrls(urls []string, parser Parser, concurrency int) []SeoData {
    tokens := make(chan struct{}, concurrency)
    worklist := make(chan []string)
    var activeTasks int
    results := []SeoData{}

    // Add the initial list of URLs to the worklist.
    activeTasks++
    go func() { worklist <- urls }()

    for activeTasks > 0 {
        batch := <-worklist
        activeTasks--

        // Loop through each URL in the batch.
        for _, url := range batch {
            if url != "" {
                activeTasks++ // Increment activeTasks for each new goroutine.

                // Process the URL concurrently.
                go func(url string) {
                    log.Printf("Requesting URL: %s", url)

                    // Scrape the URL while respecting the concurrency limit.
                    res, err := scrapePage(url, tokens, parser)
                    if err != nil {
                        log.Printf("Error scraping URL: %s", url)
                    } else {
                        results = append(results, res)
                    }

                    // Indicate that the task is complete by sending an empty batch to worklist.
                    worklist <- []string{}
                }(url)
            }
        }
    }

    return results
}


func scrapePage(url string, token chan struct{}, parser Parser) (SeoData, error) {

	res, err := func (url string, tokens chan struct{})(*http.Response, error){
		tokens <- struct{}{}
		resp, err := makeRequest(url)
		<-tokens
		if err != nil {
			return nil, err
		}
		return resp, err
	}(url, token)

	if err != nil {
		return SeoData{}, err
	}

	data, err := parser.GetSeoData(res)

	if err != nil {
		return SeoData{}, err
	}

	return data, nil
}


func ScrapeSitemap(url string, parser Parser, concurrency int) []SeoData {
	// Extract URLs from given website so that they can be crawled
	var results []string = extractSitemapURLs(url)

	// Now crawling all the URLs obtained
	var res []SeoData = scrapeUrls(results, parser, concurrency)
	return res
}

func main() {
	p := DefaultParser{}

	results := ScrapeSitemap(url, p, 10)
	for _, res := range results {
		fmt.Println(res)
	}
}
