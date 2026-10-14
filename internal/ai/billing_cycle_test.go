package ai

import (
	"math"
	"testing"
)

func TestBillingCycleNormalizationAndMonthlyCost(t *testing.T) {
	testCases := []struct {
		name              string
		price             float64
		cycle             string
		expectedCanonical string
		expectedDivisor   float64
		expectedMonthly   float64
	}{
		{
			name:              "annual 36 is 3 per month",
			price:             36.0,
			cycle:             "annual",
			expectedCanonical: "annual",
			expectedDivisor:   12.0,
			expectedMonthly:   3.0,
		},
		{
			name:              "legacy year 36 is 3 per month",
			price:             36.0,
			cycle:             "year",
			expectedCanonical: "annual",
			expectedDivisor:   12.0,
			expectedMonthly:   3.0,
		},
		{
			name:              "semiannual 60 divided by 6",
			price:             60.0,
			cycle:             "semiannual",
			expectedCanonical: "semiannual",
			expectedDivisor:   6.0,
			expectedMonthly:   10.0,
		},
		{
			name:              "legacy half_year 60 divided by 6",
			price:             60.0,
			cycle:             "half_year",
			expectedCanonical: "semiannual",
			expectedDivisor:   6.0,
			expectedMonthly:   10.0,
		},
		{
			name:              "quarter 30 divided by 3",
			price:             30.0,
			cycle:             "quarter",
			expectedCanonical: "quarter",
			expectedDivisor:   3.0,
			expectedMonthly:   10.0,
		},
		{
			name:              "biennial 240 divided by 24",
			price:             240.0,
			cycle:             "biennial",
			expectedCanonical: "biennial",
			expectedDivisor:   24.0,
			expectedMonthly:   10.0,
		},
		{
			name:              "triennial 360 divided by 36",
			price:             360.0,
			cycle:             "triennial",
			expectedCanonical: "triennial",
			expectedDivisor:   36.0,
			expectedMonthly:   10.0,
		},
		{
			name:              "free is 0 monthly cost",
			price:             100.0, // even if non-zero price is stored for free tier
			cycle:             "free",
			expectedCanonical: "free",
			expectedDivisor:   0.0,
			expectedMonthly:   0.0,
		},
		{
			name:              "month is 1x price",
			price:             15.5,
			cycle:             "month",
			expectedCanonical: "month",
			expectedDivisor:   1.0,
			expectedMonthly:   15.5,
		},
		{
			name:              "monthly legacy is 1x price",
			price:             20.0,
			cycle:             "monthly",
			expectedCanonical: "month",
			expectedDivisor:   1.0,
			expectedMonthly:   20.0,
		},
		{
			name:              "unknown cycle does not default to month and returns 0",
			price:             50.0,
			cycle:             "unknown_century",
			expectedCanonical: "",
			expectedDivisor:   0.0,
			expectedMonthly:   0.0,
		},
		{
			name:              "empty cycle returns 0",
			price:             50.0,
			cycle:             "",
			expectedCanonical: "",
			expectedDivisor:   0.0,
			expectedMonthly:   0.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			canonical, divisor := NormalizeBillingCycle(tc.cycle)
			if canonical != tc.expectedCanonical {
				t.Errorf("[%s] expected canonical %q, got %q", tc.name, tc.expectedCanonical, canonical)
			}
			if divisor != tc.expectedDivisor {
				t.Errorf("[%s] expected divisor %v, got %v", tc.name, tc.expectedDivisor, divisor)
			}
			monthly := CalculateMonthlyCost(tc.price, tc.cycle)
			if math.Abs(monthly-tc.expectedMonthly) > 1e-6 {
				t.Errorf("[%s] expected monthly cost %v, got %v", tc.name, tc.expectedMonthly, monthly)
			}
		})
	}
}
