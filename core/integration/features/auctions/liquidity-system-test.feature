Feature: Recreate a simple system test with exact setup

  Background:
    Given the following network parameters are set:
      | name                                          | value |
      | market.stake.target.timeWindow                | 10s   |
      | market.stake.target.scalingFactor             | 5     |
      | market.liquidity.targetstake.triggering.ratio | 0     |
    And the following assets are registered:
      | id  | decimal places |
      | ETH | 18             |
    And the average block duration is "1"
    And the log normal risk model named "log-normal-risk-model-1":
      | risk aversion | tau                    | mu | r     | sigma |
      | 0.001         | 0.00011407711613050422 | 0  | 0.016 | 1.5   |
    And the fees configuration named "fees-config-1":
      | maker fee | infrastructure fee |
      | 0.004     | 0.001              |
    And the margin calculator named "system-test-margin-calculator":
      | search factor | initial factor | release factor |
      | 1.1           | 1.2            | 1.4            |
    And the markets:
      | id        | quote name | asset | risk model              | margin calculator             | auction duration | fees          | price monitoring | data source config     | linear slippage factor | quadratic slippage factor | lp price range | decimal places |
      | ETH/DEC19 | ETH        | ETH   | log-normal-risk-model-1 | system-test-margin-calculator | 1                | fees-config-1 | default-none     | default-eth-for-future | 0.1                    | 0.1                       | 1              | 5              |

  @SystemTestFunk
  Scenario: Recreate system test scenario that broke after 7588 fix (missing peg)
    Given the parties deposit on asset's general account the following amount:
      | party  | asset | amount                           |
      | party0 | ETH   | 100000000000000000000000000      |
      | party1 | ETH   | 100000000000000000000000000      |
      | party2 | ETH   | 100000000000000000000000000      |
      | party3 | ETH   | 100000000000000000000000000      |
      | lp0    | ETH   | 10000000000000000000000000000000 |


    # submit our LP
    Then the parties submit the following liquidity provision:
      | id  | party  | market id | commitment amount    | fee | side | pegged reference | proportion | offset | lp type    |
      | lp1 | lp0    | ETH/DEC19 | 39050000000000000000 | 0.3 | buy  | BID              | 2          | 100000 | submission |
      | lp1 | lp0    | ETH/DEC19 | 39050000000000000000 | 0.3 | sell | ASK              | 13         | 100000 | submission |

    # get out of auction
    When the parties place the following orders:
      | party  | market id | side | volume | price  | resulting trades | type       | tif     | reference |
      | party0 | ETH/DEC19 | buy  | 5      | 1001   | 0                | TYPE_LIMIT | TIF_GTC | oa-b-1    |
      | party1 | ETH/DEC19 | sell | 5      | 951    | 0                | TYPE_LIMIT | TIF_GTC | oa-s-1    |
      | party0 | ETH/DEC19 | buy  | 5      | 900    | 0                | TYPE_LIMIT | TIF_GTC | oa-b-2    |
      | party1 | ETH/DEC19 | sell | 5      | 1200   | 0                | TYPE_LIMIT | TIF_GTC | oa-s-2    |
      | party0 | ETH/DEC19 | buy  | 1      | 100    | 0                | TYPE_LIMIT | TIF_GTC | oa-b-3    |
      | party1 | ETH/DEC19 | sell | 1      | 100100 | 0                | TYPE_LIMIT | TIF_GTC | oa-s-3    |
    Then the opening auction period ends for market "ETH/DEC19"
    And the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "ETH/DEC19"
    And the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            | target stake      | supplied stake       | open interest |
      | 976        | TRADING_MODE_CONTINUOUS | 13490760000000000 | 39050000000000000000 | 5             |

    # Check margin levels, balances, and position data
    Then the parties should have the following profit and loss:
      | party  | volume | unrealised pnl | realised pnl |
      | party0 | 5      | 0              | 0            |
      | party1 | -5     | 0              | 0            |
      | lp0    | 0      | 0              | 0            |
    And the parties should have the following margin levels:
      | party  | market id | maintenance            | search                 | initial                | release                |
      | party0 | ETH/DEC19 | 9450190458676863       | 10395209504544549      | 11340228550412235      | 13230266642147608      |
      | party1 | ETH/DEC19 | 17136958173330111      | 18850653990663122      | 20564349807996133      | 23991741442662155      |
      | lp0    | ETH/DEC19 | 2005817612830286021360 | 2206399374113314623496 | 2406981135396343225632 | 2808144657962400429904 |
    And the parties should have the following account balances:
      | party  | asset | market id | margin                 | general                         | bond                 |
      | party0 | ETH   | ETH/DEC19 | 11340228550412235      | 99999999988659771449587765      |                      |
      | party1 | ETH   | ETH/DEC19 | 20564349807996133      | 99999999979435650192003867      |                      |
      | lp0    | ETH   | ETH/DEC19 | 2406981135396343225632 | 9999999997553968864603656774368 | 39050000000000000000 |

    # add a few pegged orders now
    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference | expires in |
      | party2 | ETH/DEC19 | buy  | 1      | 970   | 0                | TYPE_LIMIT | TIF_GTC | t2-1      |            |
      | party2 | ETH/DEC19 | buy  | 1      | 980   | 0                | TYPE_LIMIT | TIF_GTT | t2-2      | 120        |
      | party3 | ETH/DEC19 | sell | 1      | 970   | 1                | TYPE_LIMIT | TIF_GTC | t3-1      |            |
      | party3 | ETH/DEC19 | sell | 1      | 980   | 0                | TYPE_LIMIT | TIF_GTC | t3-2      |            |
    Then the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            | target stake      | supplied stake       | open interest |
      | 976        | TRADING_MODE_CONTINUOUS | 16255260000000000 | 39050000000000000000 | 6             |

    When the network moves ahead "5" blocks
    # Check margin levels, balances, and position data again
    Then the parties should have the following profit and loss:
      | party  | volume | unrealised pnl | realised pnl |
      | lp0    | 0      | 0              | 0            |
      | party0 | 5      | 0              | 0            |
      | party1 | -5     | 0              | 0            |
      #| party2 | 1      | 0              | 0            |
      #| party3 | -1     | 0              | 0            |
    And the parties should have the following margin levels:
      | party  | market id | maintenance            | search                 | initial                | release                |
      | party0 | ETH/DEC19 | 9450190458676863       | 10395209504544549      | 11340228550412235      | 13230266642147608      |
      | party1 | ETH/DEC19 | 17136958173330111      | 18850653990663122      | 20564349807996133      | 23991741442662155      |
      | lp0    | ETH/DEC19 | 2014038176817295390300 | 2215441994499024929330 | 2416845812180754468360 | 2819653447544213546420 |
      | party2 | ETH/DEC19 | 1027307356123066       | 1130038091735372       | 1232768827347679       | 1438230298572292       |
      | party3 | ETH/DEC19 | 3043870903476809       | 3348257993824489       | 3652645084172170       | 4261419264867532       |
    And the parties should have the following account balances:
      | party  | asset | market id | margin                 | general                         | bond                 |
      | party0 | ETH   | ETH/DEC19 | 11340228550412235      | 99999999988659771449587765      |                      |
      | party1 | ETH   | ETH/DEC19 | 20564349807996133      | 99999999979435650192003867      |                      |
      | lp0    | ETH   | ETH/DEC19 | 2406981135396343225632 | 9999999997553971804603656774368 | 39050000000000000000 |
      | party2 | ETH   | ETH/DEC19 | 1232768827347679       | 99999999998806431172652321      |                      |
      | party3 | ETH   | ETH/DEC19 | 3652645084172170       | 99999999993358354915827830      |                      |
