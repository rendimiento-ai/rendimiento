package wsl

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// relojURL is asked the time; ahora is this computer's. Tests replace both.
var (
	relojURL = "https://api.github.com"
	ahora    = time.Now
)

// Reloj is how far this computer's clock is from the real time (positive:
// ahead), by the Date of an HTTPS answer; under a minute counts as zero.
// GitHub refuses rendimiento's signature when the clock is off.
func Reloj(ctx context.Context, c *http.Client) (time.Duration, error) {
	if c == nil {
		c = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, esperaHTTP)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, relojURL, nil)
	if err != nil {
		return 0, err
	}
	antes := ahora()
	resp, err := c.Do(req)
	if err != nil {
		return 0, fmt.Errorf("clock: %w", err)
	}
	resp.Body.Close()
	// The answer was dated somewhere between asking and hearing.
	aqui := antes.Add(ahora().Sub(antes) / 2)
	real, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return 0, fmt.Errorf("clock: %s answered without a date", relojURL)
	}
	if lejos := aqui.Sub(real).Round(time.Second); lejos <= -time.Minute || lejos >= time.Minute {
		return lejos, nil
	}
	return 0, nil
}
