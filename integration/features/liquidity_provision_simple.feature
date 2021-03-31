Feature: Test LP orders

  Background:
    Given the insurance pool initial balance for the markets is "0":
    And the execution engine have these markets:
      | name      | quote name | asset | risk model | lamd/long | tau/short | mu/max move up | r/min move down | sigma | release factor | initial factor | search factor | auction duration | maker fee | infrastructure fee | liquidity fee | p. m. update freq. | p. m. horizons | p. m. probs | p. m. durations | prob. of trading | oracle spec pub. keys | oracle spec property | oracle spec property type | oracle spec binding |
      | ETH/DEC19 | ETH        | ETH   | simple     | 0.11      | 0.1       | 0              | 0               | 0     | 1.4            | 1.2            | 1.1           | 1                | 0         | 0                  | 0             | 0                  |                |             |                 | 0.1              | 0xDEADBEEF,0xCAFEDOOD | prices.ETH.value     | TYPE_INTEGER              | prices.ETH.value    |
    And the following network parameters are set:
      | market.auction.minimumDuration |
      | 1                              |
    And oracles broadcast data signed with "0xDEADBEEF":
      | name             | value |
      | prices.ETH.value | 42    |

  Scenario: create liquidity provisions
    Given the traders make the following deposits on asset's general account:
      | trader           | asset | amount    |
      | trader1          | ETH   | 100000000 |
      | sellSideProvider | ETH   | 100000000 |
      | buySideProvider  | ETH   | 100000000 |
      | auxiliary        | ETH   | 100000000 |
      | aux2             | ETH   | 100000000 |

    When traders place the following orders:
      | trader           | market id | side | volume | price | resulting trades | type       | tif     | reference       |
      | auxiliary        | ETH/DEC19 | buy  | 1      | 80    | 0                | TYPE_LIMIT | TIF_GTC | oa-b-1          |
      | auxiliary        | ETH/DEC19 | sell | 1      | 120   | 0                | TYPE_LIMIT | TIF_GTC | oa-s-1          |
      | aux2             | ETH/DEC19 | buy  | 1      | 100   | 0                | TYPE_LIMIT | TIF_GTC | oa-b-2          |
      | auxiliary        | ETH/DEC19 | sell | 1      | 100   | 0                | TYPE_LIMIT | TIF_GTC | oa-s-2          |
    Then the opening auction period for market "ETH/DEC19" ends
    And the trading mode for the market "ETH/DEC19" is "TRADING_MODE_CONTINUOUS"

    When traders place the following orders:
      | trader           | market id | side | volume | price | resulting trades | type       | tif     | reference       |
      | sellSideProvider | ETH/DEC19 | sell | 1000   | 120   | 0                | TYPE_LIMIT | TIF_GTC | sell-provider-1 |
      | buySideProvider  | ETH/DEC19 | buy  | 1000   | 80    | 0                | TYPE_LIMIT | TIF_GTC | buy-provider-1  |
      | trader1          | ETH/DEC19 | buy  | 500    | 110   | 0                | TYPE_LIMIT | TIF_GTC | lp-ref-1        |
      | trader1          | ETH/DEC19 | sell | 500    | 120   | 0                | TYPE_LIMIT | TIF_GTC | lp-ref-2        |
    Then I see the following order events:
      | trader           | market id | side | volume | reference | offset | price | status        |
      | sellSideProvider | ETH/DEC19 | sell | 1000   |           | 0      | 120   | STATUS_ACTIVE |
      | buySideProvider  | ETH/DEC19 | buy  | 1000   |           | 0      | 80    | STATUS_ACTIVE |
    And clear order events
    Then the trader submits LP:
      | id  | party   | market id | commitment amount | fee | order side | order reference | order proportion | order offset |
      | lp1 | trader1 | ETH/DEC19 | 10000             | 0.1 | buy        | BID             | 500              | -10          |
      | lp1 | trader1 | ETH/DEC19 | 10000             | 0.1 | sell       | ASK             | 500              | 10           |
    Then I see the LP events:
      | id  | party   | market    | commitment amount | status        |
      | lp1 | trader1 | ETH/DEC19 | 10000             | STATUS_ACTIVE |
    Then I see the following order events:
      | trader  | market id | side | volume | reference | offset | price | status        |
      | trader1 | ETH/DEC19 | buy  | 450    |           | 0      | 100   | STATUS_ACTIVE |
      | trader1 | ETH/DEC19 | sell | 308    |           | 0      | 130   | STATUS_ACTIVE |
