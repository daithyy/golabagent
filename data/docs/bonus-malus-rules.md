# French Motor Bonus-Malus Rules (Reference Note)

Internal reference note written for this project. It summarises the French no-claims rules that the portfolio's bonus_malus rating factor is based on. Sources: the annex to Article A121-1 of the French Insurance Code (Code des assurances, published on Legifrance) and the French government's Service-Public.fr guide "Bonus-malus in automobile insurance".

## What the bonus-malus coefficient is

French motor insurers must apply a statutory no-claims system called the "coefficient de reduction-majoration" (CRM), usually known as bonus-malus. At each annual renewal, the premium is the insurer's reference premium multiplied by the policyholder's coefficient.

A new driver starts at a coefficient of 1.00. A coefficient below 1.00 is a bonus (a discount for a good claims record). A coefficient above 1.00 is a malus (a surcharge after at-fault claims).

## How the coefficient changes

Each year without an at-fault claim, the coefficient is multiplied by 0.95, a 5% reduction. For vehicles used for professional rounds ("tournees" or "tous deplacements") the reduction is 7% (multiplied by 0.93).

Each at-fault claim multiplies the coefficient by 1.25, a 25% increase. For professional-use vehicles the increase is 20%. If the driver is only partly at fault, the increase is halved, to 12.5%.

The result is truncated at the second decimal place.

## Limits

The coefficient cannot fall below 0.50, the maximum bonus. A driver starting at 1.00 reaches 0.50 after 13 consecutive years without an at-fault claim.

The coefficient cannot rise above 3.50, the maximum malus.

A driver who has held the maximum bonus of 0.50 for at least three years does not lose it for their first at-fault claim.

A driver in malus returns to 1.00 after two consecutive years without an at-fault claim.

## Worked example

A driver at 1.00 who has an at-fault claim moves to 1.25 at renewal. A second at-fault claim the following year takes them to 1.56 (1.5625 truncated). After that, two consecutive claim-free years return them to 1.00.

A driver at 1.00 with no at-fault claims moves to 0.95 after one year and 0.90 after two years (0.9025 truncated).

## How it appears in the portfolio data

In the portfolio data, the bonus_malus column stores the coefficient multiplied by 100, so 50 is coefficient 0.50 (the maximum bonus) and 100 is the starting level. Values below 100 are a bonus and values above 100 are a malus. The legal maximum would be 350 (coefficient 3.50), but the highest value in this portfolio is 230.

Bonus-malus is a strong rating factor because it summarises each driver's own claims history. When analysing claim frequency by bonus_malus, expect higher frequency for policies in malus. Note that the factor is partly circular: past claims raise the coefficient, and the coefficient is then used to predict future claims.
