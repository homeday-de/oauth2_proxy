package providers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
)

type GitHubProvider struct {
	*ProviderData
	Org       string
	Team      string
	userRoles []struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
		Org  struct {
			Login string `json:"login"`
		} `json:"organization"`
	}
}

func NewGitHubProvider(p *ProviderData) *GitHubProvider {
	p.ProviderName = "GitHub"
	if p.LoginURL == nil || p.LoginURL.String() == "" {
		p.LoginURL = &url.URL{
			Scheme: "https",
			Host:   "github.com",
			Path:   "/login/oauth/authorize",
		}
	}
	if p.RedeemURL == nil || p.RedeemURL.String() == "" {
		p.RedeemURL = &url.URL{
			Scheme: "https",
			Host:   "github.com",
			Path:   "/login/oauth/access_token",
		}
	}
	// ValidationURL is the API Base URL
	if p.ValidateURL == nil || p.ValidateURL.String() == "" {
		p.ValidateURL = &url.URL{
			Scheme: "https",
			Host:   "api.github.com",
			Path:   "/",
		}
	}
	if p.Scope == "" {
		p.Scope = "user:email"
	}
	return &GitHubProvider{ProviderData: p}
}

func (p *GitHubProvider) SetOrgTeam(org, team string) {
	p.Org = org
	p.Team = team
	if org != "" || team != "" {
		p.Scope += " read:org"
	}
}

func (p *GitHubProvider) hasOrg(accessToken string) (bool, error) {
	// https://developer.github.com/v3/orgs/#list-your-organizations

	var orgs []struct {
		Login string `json:"login"`
	}

	params := url.Values{
		"access_token": {accessToken},
		"limit":        {"100"},
	}

	endpoint := &url.URL{
		Scheme:   p.ValidateURL.Scheme,
		Host:     p.ValidateURL.Host,
		Path:     path.Join(p.ValidateURL.Path, "/user/orgs"),
		RawQuery: params.Encode(),
	}
	req, _ := http.NewRequest("GET", endpoint.String(), nil)

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return false, err
	}
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("got %d from %q %s", resp.StatusCode, endpoint, body)
	}

	if err := json.Unmarshal(body, &orgs); err != nil {
		return false, err
	}

	var presentOrgs []string
	for _, org := range orgs {
		if p.Org == org.Login {
			log.Printf("Found Github Organization: %q", org.Login)
			return true, nil
		}
		presentOrgs = append(presentOrgs, org.Login)
	}

	log.Printf("Missing Organization:%q in %v", p.Org, presentOrgs)
	return false, nil
}

func (p *GitHubProvider) SetUserRoles(accessToken string) (bool, error) {

	// https://developer.github.com/v3/orgs/teams/#list-user-teams
	params := url.Values{
		"access_token": {accessToken},
		"limit":        {"100"},
	}

	endpoint := &url.URL{
		Scheme:   p.ValidateURL.Scheme,
		Host:     p.ValidateURL.Host,
		Path:     path.Join(p.ValidateURL.Path, "/user/teams"),
		RawQuery: params.Encode(),
	}
	req, _ := http.NewRequest("GET", endpoint.String(), nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return false, err
	}
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("got %d from %q %s", resp.StatusCode, endpoint, body)
	}

	if err := json.Unmarshal(body, &p.userRoles); err != nil {
		return false, fmt.Errorf("%s unmarshaling %s", err, body)
	}

	log.Printf("Returned roles - %v", p.userRoles)

	return true, nil
}

func (p *GitHubProvider) hasOrgAndTeam(accessToken string) (bool, error) {

	var hasOrg bool
	presentOrgs := make(map[string]bool)
	var presentTeams []string
	for _, team := range p.userRoles {
		presentOrgs[team.Org.Login] = true
		if p.Org == team.Org.Login {
			hasOrg = true
			ts := strings.Split(p.Team, ",")
			for _, t := range ts {
				if t == team.Slug {
					log.Printf("Found Github Organization:%q Team:%q (Name:%q)", team.Org.Login, team.Slug, team.Name)
					return true, nil
				}
			}
			presentTeams = append(presentTeams, team.Slug)
		}
	}
	if hasOrg {
		log.Printf("Missing Team:%q from Org:%q in teams: %v", p.Team, p.Org, presentTeams)
	} else {
		var allOrgs []string
		for org, _ := range presentOrgs {
			allOrgs = append(allOrgs, org)
		}
		log.Printf("Missing Organization:%q in %#v", p.Org, allOrgs)
	}
	return false, nil
}

func (p *GitHubProvider) GetEmailAddress(s *SessionState) (string, error) {
	var emails []struct {
		Email   string `json:"email"`
		Primary bool   `json:"primary"`
	}

	if ok, err := p.SetUserRoles(s.AccessToken); err != nil || !ok {
		return "", err
	}

	// if we require an Org or Team, check that first
	if p.Org != "" {
		if p.Team != "" {
			if ok, err := p.hasOrgAndTeam(s.AccessToken); err != nil || !ok {
				return "", err
			}
		} else {
			if ok, err := p.hasOrg(s.AccessToken); err != nil || !ok {
				return "", err
			}
		}
	}

	params := url.Values{
		"access_token": {s.AccessToken},
	}

	endpoint := &url.URL{
		Scheme:   p.ValidateURL.Scheme,
		Host:     p.ValidateURL.Host,
		Path:     path.Join(p.ValidateURL.Path, "/user/emails"),
		RawQuery: params.Encode(),
	}
	resp, err := http.DefaultClient.Get(endpoint.String())
	if err != nil {
		return "", err
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("got %d from %q %s", resp.StatusCode, endpoint, body)
	} else {
		log.Printf("got %d from %q %s", resp.StatusCode, endpoint, body)
	}

	if err := json.Unmarshal(body, &emails); err != nil {
		return "", fmt.Errorf("%s unmarshaling %s", err, body)
	}

	for _, domain := range p.PreferredEmailDomains {
		if domain == "*" {
			break
		}
		for _, email := range emails {
			if strings.HasSuffix(email.Email, domain) {
				return email.Email, nil
			}
		}
	}
	for _, email := range emails {
		if email.Primary {
			return email.Email, nil
		}
	}

	return "", nil
}

// Return a filtered list of all teams assigned to a user by the organization defined in the configuration
func (p *GitHubProvider) GetUserRoles() string {

	// Todo - could abstract this filtering and refactor hasOrgAndTeam()
	presentOrgs := make(map[string]bool)
	var presentRoles []string
	for _, team := range p.userRoles {
		presentOrgs[team.Org.Login] = true
		if p.Org == team.Org.Login {
			ts := strings.Split(p.Team, ",")
			for _, t := range ts {
				if t == team.Slug {
					log.Printf("Found Github Organization:%q Team:%q (Name:%q)", team.Org.Login, team.Slug, team.Name)
				}
			}
			presentRoles = append(presentRoles, team.Slug)
		}
	}

	return strings.Join(presentRoles, ",")
}
