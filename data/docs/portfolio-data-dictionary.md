# Portfolio Data Dictionary

Internal reference note written for this project. It describes the underwriting portfolio in the uw schema: where it comes from, what each field means, how to calculate the two main metrics, and the data quality issues found when profiling it.

## Source and licence

The portfolio is the freMTPL2 dataset: French motor third-party liability (MTPL) policies from a French insurer, with their claims. It was published in the CASdatasets collection by C. Dutang and A. Charpentier (2018), alongside the book Computational Actuarial Science with R. It is distributed through OpenML (datasets 41214 and 41215) under a CC0 public domain licence.

It is used here as a stand-in for an underwriting portfolio. It is not Irish business, and the data has no policy dates, so it cannot show trends over time. Most policies were observed for about one year.

## Table

uw.policies has one row per policy (678,013 rows, from freMTPL2freq). Each row holds the rating factors, the exposure, the number of claims and the total amount paid in claims. The total is the sum of that policy's rows in the freMTPL2sev claims file, added up when the portfolio is loaded. There is no separate claims table.

## Rating factors

- driver_age: age of the driver in years, from 18 (the minimum driving age in France) to 100.
- bonus_malus: the French no-claims coefficient multiplied by 100. 50 is the maximum bonus, 100 is the starting level, and above 100 is a malus. See the bonus-malus rules note.
- area: how urban the driver's town is, from A (rural) to F (city centre).
- vehicle_age: vehicle age in years.
- fuel: Diesel or Regular.

The source files also have vehicle power, vehicle brand, population density and region. They are not loaded, to keep the table small.

The dataset contains no gender, which matters in the EU: since December 2012, following the EU Court of Justice's Test-Achats ruling, insurers in the EU cannot use gender as a pricing factor.

## Metric definitions

Exposure is the time a policy was in force during the observation period, in years. Claim rates must be weighted by exposure: a policy observed for a month has had less time to claim than one observed for a year.

Claim frequency is the number of claims divided by the exposure: SUM(claim_count) / SUM(exposure). It is expressed in claims per policy-year.

Pure premium (also called the burning cost or risk premium) is the expected claims cost per policy-year: SUM(total_claim_amount) / SUM(exposure). A commercial premium adds expenses, reinsurance costs and a profit margin on top.

## Data quality issues

These issues were found when profiling the loaded data. They are properties of the published source files, not loading errors.

Claim counts and claim amounts do not fully reconcile. 34,060 policies have at least one claim counted in claim_count, but 9,116 of them have no claim amounts in the claims file, so their total_claim_amount is 0. Pure premium is therefore understated.

195 claims in the claims file belong to a policy that is not in the policy file. They are left out when the portfolio is loaded.

1,224 policies have an exposure above one year (up to 2.01 years). Many analyses cap exposure at 1.

A few policies have implausibly high claim counts: the maximum is 16, and 9 policies have more than 4 claims. Many analyses cap claim counts at 4.

Claim amounts are heavily skewed. The median claim is 1,172 euro but the mean is about 2,279 euro, and the largest single claim is about 4.08 million euro. The 42 claims above 100,000 euro make up about a quarter of total claims cost. Large claims are usually capped or modelled separately, because a handful of them can dominate pure premium for any segment they fall into.
