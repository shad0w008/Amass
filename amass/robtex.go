// Copyright 2017 Jeff Foley. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package amass

import (
	"bufio"
	"encoding/json"
	"strings"
	"time"

	"github.com/OWASP/Amass/amass/utils"
)

// Robtex is the Service that handles access to the Robtex data source.
type Robtex struct {
	BaseService

	SourceType string
}

type robtexJSON struct {
	Name string `json:"rrname"`
	Data string `json:"rrdata"`
	Type string `json:"rrtype"`
}

// NewRobtex returns he object initialized, but not yet started.
func NewRobtex(e *Enumeration) *Robtex {
	r := &Robtex{SourceType: API}

	r.BaseService = *NewBaseService(e, "Robtex", r)
	return r
}

// OnStart implements the Service interface
func (r *Robtex) OnStart() error {
	r.BaseService.OnStart()

	go r.startRootDomains()
	go r.processRequests()
	return nil
}

func (r *Robtex) processRequests() {
	for {
		select {
		case <-r.PauseChan():
			<-r.ResumeChan()
		case <-r.Quit():
			return
		case <-r.RequestChan():
			// This data source just throws away the checked DNS names
			r.SetActive()
		}
	}
}

func (r *Robtex) startRootDomains() {
	// Look at each domain provided by the config
	for _, domain := range r.Enum().Config.Domains() {
		r.executeQuery(domain)
	}
}

func (r *Robtex) executeQuery(domain string) {
	var ips []string

	url := "https://freeapi.robtex.com/pdns/forward/" + domain
	page, err := utils.RequestWebPage(url, nil, nil, "", "")
	if err != nil {
		r.Enum().Log.Printf("%s: %s: %v", r.String(), url, err)
		return
	}

	for _, line := range r.parseJSON(page) {
		if line.Type == "A" {
			ips = utils.UniqueAppend(ips, line.Data)
			// Inform the Address Service of this finding
			r.Enum().NewAddressEvent(&Request{
				Domain:  domain,
				Address: line.Data,
				Tag:     r.SourceType,
				Source:  r.String(),
			})
		}
	}

	var list string
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
loop:
	for _, ip := range ips {
		r.SetActive()

		select {
		case <-r.Quit():
			break loop
		case <-t.C:
			url = "https://freeapi.robtex.com/pdns/reverse/" + ip
			pdns, err := utils.RequestWebPage(url, nil, nil, "", "")
			if err != nil {
				r.Enum().Log.Printf("%s: %s: %v", r.String(), url, err)
				continue
			}

			for _, line := range r.parseJSON(pdns) {
				list += line.Name + " "
			}
		}
	}

	r.SetActive()
	re := r.Enum().Config.DomainRegex(domain)
	for _, sd := range re.FindAllString(list, -1) {
		r.Enum().NewNameEvent(&Request{
			Name:   cleanName(sd),
			Domain: domain,
			Tag:    r.SourceType,
			Source: r.String(),
		})
	}
}

func (r *Robtex) parseJSON(page string) []robtexJSON {
	var lines []robtexJSON

	scanner := bufio.NewScanner(strings.NewReader(page))
	for scanner.Scan() {
		// Get the next line of JSON
		line := scanner.Text()
		if line == "" {
			continue
		}

		var j robtexJSON
		err := json.Unmarshal([]byte(line), &j)
		if err != nil {
			continue
		}
		lines = append(lines, j)
	}
	return lines
}
