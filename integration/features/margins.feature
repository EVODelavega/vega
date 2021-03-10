Feature: Test trader accounts

  Background:
    Given the insurance pool initial balance for the markets is "0":
    And the execution engine have these markets:
      | name      | quote name | asset | risk model | lamd/long | tau/short | mu/max move up | r/min move down | sigma | release factor | initial factor | search factor | settlement price | auction duration | maker fee | infrastructure fee | liquidity fee | p. m. update freq. | p. m. horizons | p. m. probs | p. m. durations | prob. of trading | oracle spec pub. keys | oracle spec property | oracle spec property type | oracle spec binding |
      | ETH/DEC19 | ETH        | ETH   |  simple     | 0.11      | 0.1       | 0              | 0               | 0     | 1.4            | 1.2            | 1.1           | 42               | 1                | 0         | 0                  | 0             | 0                  |                |             |                 | 0.1              | 0xDEADBEEF,0xCAFEDOOD | prices.ETH.value     | TYPE_INTEGER              | prices.ETH.value    |
    And oracles broadcast data signed with "0xDEADBEEF":
      | name             | value |
      | prices.ETH.value | 42    |
  Scenario: a trader place a new order in the system, margin are calculated
    Given the following traders:
      | name      | amount  |
      | traderGuy | 10000   |
      | trader1   | 1000000 |
      | trader2   | 1000000 |
    Then I Expect the traders to have new general account:
      | name      | asset |
      | traderGuy | ETH   |
      | trader1   | ETH   |
      | trader2   | ETH   |

    # Trigger an auction to set the mark price
    Then traders place following orders with references:
      | trader  | id        | type | volume | price | resulting trades | type       | tif     | reference |
      | trader1 | ETH/DEC19 | buy  | 1      | 10    | 0                | TYPE_LIMIT | TIF_GTC | trader1-1 |
      | trader2 | ETH/DEC19 | sell | 1      | 10000 | 0                | TYPE_LIMIT | TIF_GTC | trader2-1 |
      | trader1 | ETH/DEC19 | buy  | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GFA | trader1-2 |
      | trader2 | ETH/DEC19 | sell | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GFA | trader2-2 |
    Then the opening auction period for market "ETH/DEC19" ends
    And the mark price for the market "ETH/DEC19" is "1000"
    Then traders cancels the following orders reference:
      | trader  | reference |
      | trader1 | trader1-1 |
      | trader2 | trader2-1 |

    Then I Expect the traders to have new general account:
      | name      | asset |
      | traderGuy | ETH   |
    And "traderGuy" general accounts balance is "10000"
    Then traders place following orders:
      | trader    | id        | type | volume | price | resulting trades | type       | tif     |
      | traderGuy | ETH/DEC19 | sell | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
    Then the margins levels for the traders are:
      | trader    | id        | maintenance | search | initial | release |
      | traderGuy | ETH/DEC19 | 100         | 110    | 120     | 140     |
    Then I expect the trader to have a margin:
      | trader    | asset | id        | margin | general |
      | traderGuy | ETH   | ETH/DEC19 | 120    | 9880    |

  Scenario: an order is rejected if a trader have insufficient margin
    Given the following traders:
      | name      | amount  |
      | traderGuy | 1       |
      | trader1   | 1000000 |
      | trader2   | 1000000 |
    Then I Expect the traders to have new general account:
      | name      | asset |
      | traderGuy | ETH   |
      | trader1   | ETH   |
      | trader2   | ETH   |

    # Trigger an auction to set the mark price
    Then traders place following orders with references:
      | trader  | id        | type | volume | price | resulting trades | type       | tif     | reference |
      | trader1 | ETH/DEC19 | buy  | 1      | 10    | 0                | TYPE_LIMIT | TIF_GTC | trader1-1 |
      | trader2 | ETH/DEC19 | sell | 1      | 10000 | 0                | TYPE_LIMIT | TIF_GTC | trader2-1 |
      | trader1 | ETH/DEC19 | buy  | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GFA | trader1-2 |
      | trader2 | ETH/DEC19 | sell | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GFA | trader2-2 |
    Then the opening auction period for market "ETH/DEC19" ends
    And the mark price for the market "ETH/DEC19" is "1000"
    Then traders cancels the following orders reference:
      | trader  | reference |
      | trader1 | trader1-1 |
      | trader2 | trader2-1 |

    Then I Expect the traders to have new general account:
      | name      | asset |
      | traderGuy | ETH   |
    And "traderGuy" general accounts balance is "1"
    Then traders place following failing orders:
      | trader    | id        | type | volume | price | error               | type       |
      | traderGuy | ETH/DEC19 | sell | 1      | 1000  | margin check failed | TYPE_LIMIT |
    Then the following orders are rejected:
      | trader    | id        | reason                          |
      | traderGuy | ETH/DEC19 | ORDER_ERROR_MARGIN_CHECK_FAILED |
