package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type Config struct {
	Port              string
	ServiceBURL       string
	HTTPClientTimeout time.Duration
}

func Load(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}

	cfg := Config{
		Port:        get("PORT", "8080"),
		ServiceBURL: get("SERVICE_B_URL", "http://service-b:8081"),
	}

	var errs []error
	if p, err := strconv.Atoi(cfg.Port); err != nil || p < 1 || p > 65535 {
		errs = append(errs, errors.New("PORT must be an integer between 1 and 65535"))
	}
	if u, err := url.Parse(cfg.ServiceBURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		errs = append(errs, errors.New("SERVICE_B_URL must be an absolute http(s) URL"))
	}

	timeout, err := time.ParseDuration(get("HTTP_CLIENT_TIMEOUT", "15s"))
	if err != nil || timeout <= 0 {
		errs = append(errs, errors.New("HTTP_CLIENT_TIMEOUT must be a positive duration such as 15s"))
	}
	cfg.HTTPClientTimeout = timeout

	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) String() string {
	return fmt.Sprintf("port=%s service_b=%s timeout=%s", c.Port, c.ServiceBURL, c.HTTPClientTimeout)
}
