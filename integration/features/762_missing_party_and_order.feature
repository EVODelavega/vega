Feature: Test crash on cancel of missing order

  Background:
    Given the insurance pool initial balance for the markets is "0":
    And the markets:
      | id        | quote name | asset | risk model                  | margin calculator         | auction duration | fees         | price monitoring | oracle config          |
      | ETH/DEC19 | BTC        | BTC   | default-simple-risk-model-2 | default-margin-calculator | 0                | default-none | default-none     | default-eth-for-future |
    And oracles broadcast data signed with "0xDEADBEEF":
      | name             | value |
      | prices.ETH.value | 42    |

  Scenario: A non-existent party attempts to place an order
    When traders place the following orders:
      | trader        | market id | side | volume | price | resulting trades | type       | tif     | reference     |
      | missingTrader | ETH/DEC19 | sell | 1000   | 120   | 0                | TYPE_LIMIT | TIF_GTC | missing-ref-1 |
    Then the system should return error "trader does not exist"
    When traders cancel the following orders:
      | trader        | reference     |
      | missingTrader | missing-ref-1 |
    Then the system should return error "unable to find the order in the market"
    When traders place the following orders:
      | trader        | market id | side | volume | price | resulting trades | type       | tif     | reference     |
      | missingTrader | ETH/DEC19 | sell | 1000   | 120   | 0                | TYPE_LIMIT | TIF_GTC | missing-ref-2 |
    Then the system should return error "trader does not exist"
    When traders cancel the following orders:
      | trader        | reference     |
      | missingTrader | missing-ref-2 |
    Then the system should return error "unable to find the order in the market"
