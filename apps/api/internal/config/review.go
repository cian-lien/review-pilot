package config

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"reviewpilot.local/api/internal/contracts"
)

type ReviewModelConfig struct {
	ModelConfigVersion         string `json:"modelConfigVersion"`
	PriceVersion               string `json:"priceVersion"`
	BaseURL                    string `json:"baseURL"`
	Model                      string `json:"model"`
	StructuredOutputMode       string `json:"structuredOutputMode"`
	Currency                   string `json:"currency"`
	InputMicrosPerMillion      string `json:"inputMicrosPerMillion"`
	OutputMicrosPerMillion     string `json:"outputMicrosPerMillion"`
	RunLimitMicros             string `json:"runLimitMicros"`
	RepositoryDailyLimitMicros string `json:"repositoryDailyLimitMicros"`
	GlobalMonthlyLimitMicros   string `json:"globalMonthlyLimitMicros"`
	UsageSupported             bool   `json:"usageSupported"`
	OnlyTokenBilling           bool   `json:"onlyTokenBilling"`
	FixtureID                  string `json:"fixtureId"`
}

type ReviewConfig struct {
	Mode           string
	RealAllowed    bool
	PublishEnabled bool
	Model          ReviewModelConfig
	Policy         contracts.ReviewPolicy
}

func DefaultReviewPolicy() contracts.ReviewPolicy {
	return contracts.ReviewPolicy{MaxChangedLines: 1500, MaxFiles: 64, MaxFileBytes: 131072,
		MaxRawBytes: 2097152, MaxPageBytes: 262144, MaxStageBytes: 262144, MaxResultBytes: 65536,
		MaxCandidates: 10, MaxReferences: 5, MaxSnippetBytes: 2048, MaxInputTokens: 12000,
		MaxOutputTokens: 4096, TimeoutSeconds: 60, RetainContent: true}
}

func ParseMicros(value string) (int64, error) {
	if !regexp.MustCompile(`^(0|[1-9][0-9]{0,18})$`).MatchString(value) {
		return 0, errors.New("INVALID_MONEY")
	}
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errors.New("INVALID_MONEY")
	}
	return amount, nil
}

func LoadReview() (ReviewConfig, error) {
	c := ReviewConfig{Mode: value("REVIEW_MODE", "disabled"), Policy: DefaultReviewPolicy()}
	var err error
	c.Policy.RetainContent, err = strconv.ParseBool(value("REVIEW_RETAIN_CONTENT", "true"))
	if err != nil {
		return ReviewConfig{}, errors.New("INVALID_REVIEW_GATE")
	}
	for name, target := range map[string]*bool{"REVIEW_REAL_ALLOWED": &c.RealAllowed, "GITHUB_REVIEW_PUBLISH_ENABLED": &c.PublishEnabled} {
		*target, err = strconv.ParseBool(value(name, "false"))
		if err != nil {
			return ReviewConfig{}, errors.New("INVALID_REVIEW_GATE")
		}
	}
	if c.Mode != "disabled" && c.Mode != "fake" && c.Mode != "real" {
		return ReviewConfig{}, errors.New("INVALID_REVIEW_MODE")
	}
	if c.Mode == "real" && (!c.RealAllowed || os.Getenv("REVIEW_FAKE_SCRIPT_FILE") != "") {
		return ReviewConfig{}, errors.New("REAL_REVIEW_NOT_ALLOWED")
	}
	if c.Mode != "real" && c.PublishEnabled {
		return ReviewConfig{}, errors.New("INVALID_PUBLICATION_GATE")
	}
	path := os.Getenv("REVIEW_MODEL_CONFIG_FILE")
	if path == "" {
		if c.Mode != "disabled" {
			return ReviewConfig{}, errors.New("MODEL_CONFIG_REQUIRED")
		}
		return c, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return ReviewConfig{}, errors.New("MODEL_CONFIG_UNAVAILABLE")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(data) > 16384 {
		return ReviewConfig{}, errors.New("INVALID_MODEL_CONFIG")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c.Model) != nil || decoder.Decode(new(any)) != io.EOF {
		return ReviewConfig{}, errors.New("INVALID_MODEL_CONFIG")
	}
	m := c.Model
	version := regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)
	if !version.MatchString(m.ModelConfigVersion) || !version.MatchString(m.PriceVersion) || m.Model == "" || len(m.Model) > 128 ||
		(m.StructuredOutputMode != "json-schema" && m.StructuredOutputMode != "json-object") || !m.UsageSupported || !m.OnlyTokenBilling {
		return ReviewConfig{}, errors.New("UNSUPPORTED_MODEL_CONFIG")
	}
	for _, amount := range []string{m.InputMicrosPerMillion, m.OutputMicrosPerMillion, m.RunLimitMicros, m.RepositoryDailyLimitMicros, m.GlobalMonthlyLimitMicros} {
		n, e := ParseMicros(amount)
		if e != nil || n <= 0 {
			return ReviewConfig{}, errors.New("INVALID_MODEL_PRICE_OR_LIMIT")
		}
	}
	u, e := url.Parse(m.BaseURL)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/v1") {
		return ReviewConfig{}, errors.New("INVALID_MODEL_ADDRESS")
	}
	if c.Mode == "fake" {
		// A loopback address alone is not proof that this process owns a fixture service.
		if os.Getenv("REVIEW_TEST_ISOLATED") != "1" || m.FixtureID == "" || m.FixtureID != os.Getenv("REVIEW_TEST_FIXTURE_ID") ||
			m.BaseURL != os.Getenv("TEST_MODEL_BASE_URL") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") || u.Scheme != "http" || m.Currency != "TEST" {
			return ReviewConfig{}, errors.New("FAKE_REVIEW_NOT_ISOLATED")
		}
	} else if u.Scheme != "https" || !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(m.Currency) || m.FixtureID != "" {
		return ReviewConfig{}, errors.New("INVALID_REAL_MODEL_CONFIG")
	}
	return c, nil
}
