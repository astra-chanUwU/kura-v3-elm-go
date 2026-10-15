package client

import (
	"errors"
	"sort"
	"strings"
)

func (c *Client) secretValues() []string {
	if c == nil {
		return nil
	}
	var secrets []string
	if token := strings.TrimSpace(c.APIToken); token != "" {
		secrets = append(secrets, token, "Bearer "+token, "bearer "+token)
	}
	if value := strings.TrimSpace(c.SessionCookieValue); value != "" {
		secrets = append(secrets, value)
		if name := strings.TrimSpace(c.SessionCookieName); name != "" {
			secrets = append(secrets, name+"="+value)
		}
	}
	return secrets
}

func (c *Client) redact(msg string) string {
	return redactText(msg, c.secretValues())
}

func redactText(msg string, secrets []string) string {
	seen := map[string]struct{}{}
	list := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		if _, ok := seen[secret]; ok {
			continue
		}
		seen[secret] = struct{}{}
		list = append(list, secret)
	}
	sort.Slice(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	for _, secret := range list {
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
	}
	return msg
}

// scrub removes authorization and cookie material from an error while
// preserving API and uncertain-write error types.
func (c *Client) scrub(err error) error {
	if err == nil || c == nil {
		return err
	}
	switch e := err.(type) {
	case *APIError:
		e.Message = c.redact(e.Message)
		e.Body = c.redact(e.Body)
		e.Code = c.redact(e.Code)
		return e
	case *RequestError:
		e.Err = c.scrub(e.Err)
		if e.Err != nil {
			redacted := c.redact(e.Err.Error())
			if redacted != e.Err.Error() {
				e.Err = errors.New(redacted)
			}
		}
		return e
	default:
		redacted := c.redact(err.Error())
		if redacted == err.Error() {
			return err
		}
		return errors.New(redacted)
	}
}
