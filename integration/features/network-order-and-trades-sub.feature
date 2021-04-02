Feature: Ensure network trader are generated

  Background:
    Given the insurance pool initial balance for the markets is "0":
    And the markets:
      | id        | quote name | asset | risk model                  | margin calculator                  | auction duration | fees         | price monitoring | oracle config          |
      | ETH/DEC19 | BTC        | BTC   | default-simple-risk-model-2 | default-overkill-margin-calculator | 1                | default-none | default-none     | default-eth-for-future |
    And the following network parameters are set:
      | market.auction.minimumDuration |
      | 1                              |
    And oracles broadcast data signed with "0xDEADBEEF":
      | name             | value |
      | prices.ETH.value | 42    |

  Scenario: Implement trade and order network
# setup accounts
    Given the traders make the following deposits on asset's general account:
      | trader           | asset | amount        |
      | sellSideProvider | BTC   | 1000000000000 |
      | buySideProvider  | BTC   | 1000000000000 |
      | designatedLooser | BTC   | 12000         |
      | aux              | BTC   | 1000000000000 |
      | aux2             | BTC   | 1000000000000 |

# insurance pool generation - setup orderbook
    When traders place the following orders:
      | trader           | market id | side | volume | price | resulting trades | type       | tif     | reference       |
      | sellSideProvider | ETH/DEC19 | sell | 290    | 150   | 0                | TYPE_LIMIT | TIF_GTC | sell-provider-1 |
      | buySideProvider  | ETH/DEC19 | buy  | 1      | 140   | 0                | TYPE_LIMIT | TIF_GTC | buy-provider-1  |
      | aux              | ETH/DEC19 | sell | 100    | 159   | 0                | TYPE_LIMIT | TIF_GTC | aux-s-1         |
      | aux              | ETH/DEC19 | sell | 1      | 149   | 0                | TYPE_LIMIT | TIF_GTC | aux-s-2         |
      | aux2             | ETH/DEC19 | buy  | 1      | 149   | 0                | TYPE_LIMIT | TIF_GTC | aux-b-1         |
      | aux2             | ETH/DEC19 | buy  | 100    | 140   | 0                | TYPE_LIMIT | TIF_GTC | aux-b-2         |
    Then the opening auction period for market "ETH/DEC19" ends
    And the trading mode for the market "ETH/DEC19" is "TRADING_MODE_CONTINUOUS"

# insurance pool generation - trade
    When traders place the following orders:
      | trader           | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | designatedLooser | ETH/DEC19 | buy  | 290    | 150   | 1                | TYPE_LIMIT | TIF_GTC | ref-1     |
    And the trading mode for the market "ETH/DEC19" is "TRADING_MODE_CONTINUOUS"

# insurance pool generation - modify order book
    Then traders cancel the following orders:
      | trader          | reference      |
      | buySideProvider | buy-provider-1 |
    When traders place the following orders:
      | trader          | market id | side | volume | price | resulting trades | type       | tif     | reference      |
      | buySideProvider | ETH/DEC19 | buy  | 400    | 40    | 0                | TYPE_LIMIT | TIF_GTC | buy-provider-2 |
    And traders cancel the following orders:
      | trader          | reference      |
      | aux2            | aux-b-2        |
    And the trading mode for the market "ETH/DEC19" is "TRADING_MODE_CONTINUOUS"

# insurance pool generation - set new mark price (and trigger closeout)
    When traders place the following orders:
      | trader           | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | sellSideProvider | ETH/DEC19 | sell | 1      | 120   | 0                | TYPE_LIMIT | TIF_GTC | ref-1     |
      | buySideProvider  | ETH/DEC19 | buy  | 1      | 120   | 1                | TYPE_LIMIT | TIF_GTC | ref-2     |
    And the trading mode for the market "ETH/DEC19" is "TRADING_MODE_CONTINUOUS"

    And debug trades
# check the network trade happened
    Then the following network trades happened:
      | trader           | aggressor side | volume |
      | designatedLooser | buy            | 290    |
      | buySideProvider  | sell           | 290    |
