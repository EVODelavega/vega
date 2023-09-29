Feature: If we are below target stake, everyone gets the penalty

# For a market with market.liquidity.earlyExitPenalty = 0.25 and target stake > total stake,
# a liquidity provider who reduces their commitment by 100 will only receive 75 back into their
# general account with 25 transferred into the market's insurance account. (0044-LIME-023)

  Background:
    Given the following network parameters are set:
      | name                                                  | value |
      | market.stake.target.timeWindow                        | 24h   |
      | market.stake.target.scalingFactor                     | 1     |
      | market.liquidity.bondPenaltyParameter                 | 1     |
      | network.markPriceUpdateMaximumFrequency               | 0s    |
      | limits.markets.maxPeggedOrders                        | 2     |
      | validators.epoch.length                               | 5s    |
      | market.liquidity.earlyExitPenalty                     | 0.25  |
      | market.liquidity.stakeToCcyVolume                     | 1.0   |
      | market.liquidity.sla.nonPerformanceBondPenaltySlope   | 0.19  |
      | market.liquidity.sla.nonPerformanceBondPenaltyMax     | 1     |
    And the liquidity monitoring parameters:
      | name               | triggering ratio | time window | scaling factor |
      | lqm-params         | 0.1              | 24h         | 1              |  
    
    And the average block duration is "1"
    And the simple risk model named "simple-risk-model-1":
      | long | short | max move up | min move down | probability of trading |
      | 0.1  | 0.1   | 60          | 50            | 0.2                    |
    And the fees configuration named "fees-config-1":
      | maker fee | infrastructure fee |
      | 0.004     | 0.001              |
    And the price monitoring named "price-monitoring-1":
      | horizon | probability | auction extension |
      | 1       | 0.99        | 5                 |
    And the liquidity sla params named "SLA":
      | price range | commitment min time fraction | performance hysteresis epochs | sla competition factor |
      | 0.01        | 0.5                          | 1                             | 1.0                    |
    And the markets:
      | id        | quote name | asset | liquidity monitoring | risk model          | margin calculator         | auction duration | fees          | price monitoring   | data source config     | linear slippage factor | quadratic slippage factor | sla params |
      | ETH/DEC21 | ETH        | ETH   | lqm-params           | simple-risk-model-1 | default-margin-calculator | 1                | fees-config-1 | price-monitoring-1 | default-eth-for-future | 0.5                    | 0                         | SLA        |
    And the parties deposit on asset's general account the following amount:
      | party  | asset | amount     |
      | party1 | ETH   | 100000000  |
      | party2 | ETH   | 100000000  |
      | party3 | ETH   | 100000000  |
      | party4 | ETH   | 100000000  |
    And the parties submit the following liquidity provision:
      | id  | party  | market id | commitment amount | fee   | lp type     |
      | lp1 | party1 | ETH/DEC21 | 500               | 0.001 | submission  |
      | lp2 | party2 | ETH/DEC21 | 500               | 0.001 | submission  |
    And the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | party3 | ETH/DEC21 | buy  | 1000   | 900   | 0                | TYPE_LIMIT | TIF_GTC | p3b1      |
      | party3 | ETH/DEC21 | buy  | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC | p3b2      |
      | party4 | ETH/DEC21 | sell | 1000   | 1100  | 0                | TYPE_LIMIT | TIF_GTC | p4s1      |
      | party4 | ETH/DEC21 | sell | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC | p4s2      |

  Scenario: 001, Every commitment reduction costs us 25% of the amount
   When the opening auction period ends for market "ETH/DEC21"
   And the auction ends with a traded volume of "10" at a price of "1000"
   Then the market data for the market "ETH/DEC21" should be:
     | mark price | trading mode            | target stake | supplied stake | open interest | best static bid price | static mid price | best static offer price |
     | 1000       | TRADING_MODE_CONTINUOUS | 1000         | 1000           | 10            | 900                   | 1000             | 1100                    |
   # Place LIMIT orders to cover our commitment
   And the parties place the following orders:
     | party  | market id | side | volume | price | resulting trades | type       | tif     | reference     |
     | party1 | ETH/DEC21 | buy  | 10     | 999   | 0                | TYPE_LIMIT | TIF_GTC | party1-order1 |
     | party1 | ETH/DEC21 | sell | 10     | 1001  | 0                | TYPE_LIMIT | TIF_GTC | party1-order2 |
     | party2 | ETH/DEC21 | buy  | 10     | 999   | 0                | TYPE_LIMIT | TIF_GTC | party2-order1 |
     | party2 | ETH/DEC21 | sell | 10     | 1001  | 0                | TYPE_LIMIT | TIF_GTC | party2-order2 |
   When the network moves ahead "1" blocks
   Then the liquidity provisions should have the following states:
     | id  | party  | market    | commitment amount | status           |
     | lp1 | party1 | ETH/DEC21 | 500               | STATUS_ACTIVE    |
     | lp2 | party2 | ETH/DEC21 | 500               | STATUS_ACTIVE    |
   And the parties should have the following account balances:
     | party  | asset | market id | margin  | general  | bond  |
     | party1 | ETH   | ETH/DEC21 | 1200    | 99998300 | 500   |    
     | party2 | ETH   | ETH/DEC21 | 1200    | 99998300 | 500   |    
   And the insurance pool balance should be "0" for the market "ETH/DEC21"

   # Now reduce the commitment for party 1
   And the parties submit the following liquidity provision:
    | id  | party  | market id | commitment amount | fee   | lp type     |
    | lp1 | party1 | ETH/DEC21 | 400               | 0.001 | amendment   |
   When the network moves ahead "7" blocks

   Then the liquidity provisions should have the following states:
     | id  | party  | market    | commitment amount | status           |
     | lp1 | party1 | ETH/DEC21 | 400               | STATUS_ACTIVE    |

   And the parties should have the following account balances:
     | party  | asset | market id | margin  | general  | bond  |
     | party1 | ETH   | ETH/DEC21 | 1200    | 99998375 | 400   |   
   # party1 should give us 25% of the 100 they reduced by (25 added to insurance pool)  
   And the insurance pool balance should be "25" for the market "ETH/DEC21"

   # Now reduce the commitment for party 2
   And the parties submit the following liquidity provision:
    | id  | party  | market id | commitment amount | fee   | lp type     |
    | lp2 | party2 | ETH/DEC21 | 100               | 0.001 | amendment   |
   When the network moves ahead "7" blocks

   Then the liquidity provisions should have the following states:
     | id  | party  | market    | commitment amount | status           |
     | lp2 | party2 | ETH/DEC21 | 100               | STATUS_ACTIVE    |

   And the parties should have the following account balances:
     | party  | asset | market id | margin  | general  | bond  |
     | party2 | ETH   | ETH/DEC21 | 1200    | 99998600 | 100   |    
   # party2 should give up 25% of 400 (add 100 to the insurance pool)
   And the insurance pool balance should be "125" for the market "ETH/DEC21"

