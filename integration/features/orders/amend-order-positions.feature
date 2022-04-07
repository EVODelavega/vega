Feature: Amend order updates positions correctly

  Background:

    And the markets:
      | id        | quote name | asset | risk model                  | margin calculator                  | auction duration | fees         | price monitoring | oracle config          |
      | ETH/DEC19 | BTC        | BTC   | default-simple-risk-model-2 | default-overkill-margin-calculator | 1                | default-none | default-none     | default-eth-for-future |
    And the following network parameters are set:
      | name                           | value |
      | market.auction.minimumDuration | 1     |

  Scenario: Basic amend of orders updates positions as expected
# setup accounts
    Given the parties deposit on asset's general account the following amount:
      | party  | asset | amount    |
      | myboi  | BTC   | 100000000 |
      | aux    | BTC   | 100000000 |
      | aux2   | BTC   | 100000000 |
      | aux3   | BTC   | 100000000 |

    # place auxiliary orders so we always have best bid and best offer as to not trigger the liquidity auction
    When the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux   | ETH/DEC19 | buy  | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1      | 10001 | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC19 | sell | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC19 | buy  | 1      | 500   | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1      | 10002 | 0                | TYPE_LIMIT | TIF_GTC |
    Then the opening auction period ends for market "ETH/DEC19"
    And the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "ETH/DEC19"
    And the auction ends with a traded volume of "1" at a price of "1000"

    When the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     | reference   |
      | myboi | ETH/DEC19 | sell | 15     | 970   | 0                | TYPE_LIMIT | TIF_GTC | myboi-ref-1 |
      | aux3  | ETH/DEC19 | buy  | 10     | 960   | 0                | TYPE_LIMIT | TIF_GTC | aux3-bref-1 |
    Then the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            |
      | 1000       | TRADING_MODE_CONTINUOUS |
    And the parties should have the following profit and loss:
      | party | volume | unrealised pnl | realised pnl |
      | aux   | 1      | 0              | 0            |
      | aux2  | -1     | 0              | 0            |

    # Nothing should change when we move forward in time
    When the network moves ahead "1" blocks
    Then the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            |
      | 1000       | TRADING_MODE_CONTINUOUS |
    And the parties should have the following profit and loss:
      | party | volume | unrealised pnl | realised pnl |
      | aux   | 1      | 0              | 0            |
      | aux2  | -1     | 0              | 0            |

    When the parties amend the following orders:
      | party | reference   | price | size delta | tif     |
      | aux3  | aux3-bref-1 | 970   | 0          | TIF_GTC |
    Then the following trades should be executed:
      | buyer | seller | price | size |
      | aux3  | myboi  | 970   | 10   |
    And the parties should have the following profit and loss:
      | party | volume | unrealised pnl | realised pnl |
      | myboi | -10    | 0              | 0            |
      | aux3  | 10     | 0              | 0            |

  @AmendPos
  Scenario: Basic amend of orders updates positions as expected, even if mark price moves in the mean time
# setup accounts
    Given the parties deposit on asset's general account the following amount:
      | party  | asset | amount    |
      | myboi  | BTC   | 100000000 |
      | aux    | BTC   | 100000000 |
      | aux2   | BTC   | 100000000 |
      | aux3   | BTC   | 100000000 |
      | aux4   | BTC   | 100000000 |
      | aux5   | BTC   | 100000000 |

    # place auxiliary orders so we always have best bid and best offer as to not trigger the liquidity auction
    When the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux   | ETH/DEC19 | buy  | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1      | 10001 | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC19 | sell | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC19 | buy  | 1      | 500   | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1      | 10002 | 0                | TYPE_LIMIT | TIF_GTC |
    Then the opening auction period ends for market "ETH/DEC19"
    And the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "ETH/DEC19"
    And the auction ends with a traded volume of "1" at a price of "1000"

    # Have some other random parties move mark price to set realised P&L values
    # First place the orders that will uncross at 1010, then put in the sell order at 965
    # It needs to be placed later, because otherwise the buy order will result in 2 trades
    When the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux4  | ETH/DEC19 | sell | 10     | 1010  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux5  | ETH/DEC19 | buy  | 10     | 1010  | 1                | TYPE_LIMIT | TIF_GTC |
      | aux4  | ETH/DEC19 | sell | 1      | 965   | 0                | TYPE_LIMIT | TIF_GTC |
    Then the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            |
      | 1010       | TRADING_MODE_CONTINUOUS |

    When the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     | reference   |
      | myboi | ETH/DEC19 | sell | 15     | 970   | 0                | TYPE_LIMIT | TIF_GTC | myboi-ref-1 |
      | aux3  | ETH/DEC19 | buy  | 10     | 960   | 0                | TYPE_LIMIT | TIF_GTC | aux3-bref-1 |
    Then the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            |
      | 1010       | TRADING_MODE_CONTINUOUS |
    And the parties should have the following profit and loss:
      | party | volume | unrealised pnl | realised pnl |
      | aux   | 1      | 10             | 0            |
      | aux2  | -1     | -10            | 0            |

    # Nothing should change when we move forward in time
    When the network moves ahead "1" blocks
    And the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux5  | ETH/DEC19 | buy  | 1      | 965   | 1                | TYPE_LIMIT | TIF_GTC |
    Then the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            |
      | 965        | TRADING_MODE_CONTINUOUS |
    And the parties should have the following profit and loss:
      | party | volume | unrealised pnl | realised pnl |
      | aux   | 1      | -35            | 0            |
      | aux2  | -1     | 35             | 0            |

    When the parties amend the following orders:
      | party | reference   | price | size delta | tif     |
      | aux3  | aux3-bref-1 | 970   | 0          | TIF_GTC |
    Then the following trades should be executed:
      | buyer | seller | price | size |
      | aux3  | myboi  | 970   | 10   |
    Then the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            |
      | 970        | TRADING_MODE_CONTINUOUS |
    And the parties should have the following profit and loss:
      | party | volume | unrealised pnl | realised pnl |
      | myboi | -10    | 0              | 0            |
      | aux3  | 10     | 0              | 0            |
      | aux   | 1      | -30            | 0            |
      | aux2  | -1     | 30             | 0            |
      | aux4  | -11    | 395            | 0            |
      | aux5  | 11     | -395           | 0            |
    # Add step to zero-out one of the parties (see realised P&L and position set to zero)
    When the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux3  | ETH/DEC19 | sell | 10     | 969   | 0                | TYPE_LIMIT | TIF_GTC |
      | aux4  | ETH/DEC19 | buy  | 10     | 969   | 1                | TYPE_LIMIT | TIF_GTC |
    Then the following trades should be executed:
      | buyer | seller | price | size |
      | aux4  | aux3   | 969   | 10   |
    Then the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            |
      | 969        | TRADING_MODE_CONTINUOUS |
    And the parties should have the following profit and loss:
      | party | volume | unrealised pnl | realised pnl |
      | myboi | -10    | 10             | 0            |
      | aux3  | 0      | 0              | -10          |
      | aux   | 1      | -31            | 0            |
      | aux2  | -1     | 31             | 0            |
      | aux4  | -1     | 37             | 369          |
      | aux5  | 11     | -406           | 0            |
      # And debug all events
