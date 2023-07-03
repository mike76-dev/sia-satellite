package satellite

import (
	"errors"
	"time"

	"github.com/mike76-dev/sia-satellite/external"
)

const (
	// exchangeRateFetchInterval is how often currency exchange rates are
	// retrieved.
	exchangeRateFetchInterval = 24 * time.Hour

	// scusdRateFetchInterval is how often SC-USD rate is retrieved.
	scusdRateFetchInterval = 10 * time.Minute
)

// fetchExchangeRates retrieves the fiat currency exchange rates.
func (s *Satellite) fetchExchangeRates() {
	data, err := external.FetchExchangeRates()
	if err != nil {
		s.log.Println("ERROR:", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for k, v := range data {
		s.exchRates[k] = v
	}
}

// threadedFetchExchangeRates performs the fetch with set intervals.
func (s *Satellite) threadedFetchExchangeRates() {
	err := s.threads.Add()
	if err != nil {
		return
	}
	defer s.threads.Done()

	s.fetchExchangeRates()

	for {
		select {
		case <-s.threads.StopChan():
			return
		case <-time.After(exchangeRateFetchInterval):
		}

		s.fetchExchangeRates()
	}
}

// fetchSCUSDRate retrieves the SC-USD rate.
func (s *Satellite) fetchSCUSDRate() {
	data, err := external.FetchSCUSDRate()
	if err != nil {
		s.log.Println("ERROR:", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.scusdRate = data
}

// threadedFetchSCUSDRate performs the fetch with set intervals.
func (s *Satellite) threadedFetchSCUSDRate() {
	err := s.threads.Add()
	if err != nil {
		return
	}
	defer s.threads.Done()

	s.fetchSCUSDRate()

	for {
		select {
		case <-s.threads.StopChan():
			return
		case <-time.After(scusdRateFetchInterval):
		}

		s.fetchSCUSDRate()
	}
}

// GetSiacoinRate calculates the SC price in a given currency.
func (s *Satellite) GetSiacoinRate(currency string) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fiatRate, ok := s.exchRates[currency]
	if !ok {
		return 0, errors.New("unsupported currency")
	}

	return fiatRate * s.scusdRate, nil
}

// GetExchangeRate returns the exchange rate of a given currency.
func (s *Satellite) GetExchangeRate(currency string) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rate, ok := s.exchRates[currency]
	if !ok {
		return 0, errors.New("unsupported currency")
	}

	return rate, nil
}
