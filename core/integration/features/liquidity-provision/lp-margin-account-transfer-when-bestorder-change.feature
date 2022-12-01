
Feature:  test 0038-OLIQ-008
  Background:
    Given the log normal risk model named "lognormal-risk-model-fish":
      | risk aversion | tau  | mu | r   | sigma |
      | 0.001         | 0.01 | 0  | 0.0 | 2     |
    And the price monitoring named "price-monitoring-1":
      | horizon | probability       | auction extension |
      | 36000   | 0.999999999999999 | 300               |
    And the margin calculator named "margin-calculator-1":
      | search factor | initial factor | release factor |
      | 1.2           | 1.5            | 2              |

    And the markets:
      | id        | quote name | asset | risk model                | margin calculator   | auction duration | fees         | price monitoring   | data source config     |
      | ETH/DEC19 | ETH        | USD   | lognormal-risk-model-fish | margin-calculator-1 | 1                | default-none | price-monitoring-1 | default-eth-for-future |

    And the following network parameters are set:
      | name                                                   | value |
      | market.liquidity.minimum.probabilityOfTrading.lpOrders | 0.01  |
      | network.markPriceUpdateMaximumFrequency                | 0s    |
    And the average block duration is "1"

  Scenario: If best bid / ask has changed and the LP order volume is moved around to match the shape / new peg levels then the margin requirement for the party may change. There is at most one transfer in / out of the margin account of the LP party as a result of one of the pegs moving. 0038-OLIQ-008

    Given the parties deposit on asset's general account the following amount:
      | party | asset | amount        |
      | aux   | USD   | 1000000000000 |
      | aux2  | USD   | 1000000000000 |
      | aux3  | USD   | 1000000000000 |
      | aux4  | USD   | 1000000000000 |
      | lp    | USD   | 1000000000000 |

    When the parties submit the following liquidity provision:
      | id  | party | market id | commitment amount | fee | side | pegged reference | proportion | offset | lp type    |
      | lp1 | lp    | ETH/DEC19 | 90000             | 0.1 | buy  | BID              | 50         | 10     | submission |
      | lp1 | lp    | ETH/DEC19 | 90000             | 0.1 | sell | ASK              | 50         | 10     | submission |

    Then the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | aux3  | ETH/DEC19 | buy  | 10     | 140   | 0                | TYPE_LIMIT | TIF_GTC | bestBid   |
      | aux4  | ETH/DEC19 | sell | 10     | 160   | 0                | TYPE_LIMIT | TIF_GTC | bestOffer |
      | aux   | ETH/DEC19 | buy  | 1      | 150   | 0                | TYPE_LIMIT | TIF_GTC |           |
      | aux2  | ETH/DEC19 | sell | 1      | 150   | 0                | TYPE_LIMIT | TIF_GTC |           |
    Then the opening auction period ends for market "ETH/DEC19"
    And the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            | auction trigger             | horizon | min bound | max bound |
      | 150        | TRADING_MODE_CONTINUOUS | AUCTION_TRIGGER_UNSPECIFIED | 36000   | 88        | 257       |

    And the liquidity provisions should have the following states:
      | id  | party | market    | commitment amount | status        |
      | lp1 | lp    | ETH/DEC19 | 90000             | STATUS_ACTIVE |

    # Observe that given specified pegs we should have an LP buy order placed at a price of 1 and sell order placed at a price of 3160, however, since both of these fall outside of price monitoring bounds the orders gets moved accordingly (0038-OLIQ-009)
    Then the order book should have the following volumes for market "ETH/DEC19":
      | side | price | volume |
      | sell | 170   | 1419   |
      | sell | 160   | 10     |
      | buy  | 140   | 10     |
      | buy  | 130   | 1901   |

    Then the parties should have the following margin levels:
      | party | market id | maintenance | search | initial | release |
      | aux3  | ETH/DEC19 | 750         | 900    | 1125    | 1500    |
      | aux4  | ETH/DEC19 | 1388        | 1665   | 2082    | 2776    |
      | lp    | ETH/DEC19 | 196841      | 236209 | 295261  | 393682  |

    Then the parties should have the following account balances:
      | party | asset | market id | margin | general      |
      | aux3  | USD   | ETH/DEC19 | 1050   | 999999998950 |
      | aux4  | USD   | ETH/DEC19 | 2220   | 999999997780 |
      | lp    | USD   | ETH/DEC19 | 295261 | 999999614739 |

    When the following network parameters are set:
      | name                                                   | value |
      | market.liquidity.minimum.probabilityOfTrading.lpOrders | 0.1   |
    And the network moves ahead "10" blocks

    # updating the parameter itself is not enough for the volumes to get affected
    Then the order book should have the following volumes for market "ETH/DEC19":
      | side | price | volume |
      | sell | 170   | 1419   |
      | sell | 160   | 10     |
      | buy  | 140   | 10     |
      | buy  | 130   | 1901   |

    # update the best offer
    When the parties amend the following orders:
      | party | reference | price | size delta | tif     |
      | aux4  | bestOffer | 165   | 0          | TIF_GTC |

    # observe volumes change
    Then the order book should have the following volumes for market "ETH/DEC19":
      | side | price | volume |
      | sell | 175   | 1366   |
      | sell | 165   | 10     |
      | buy  | 140   | 10     |
      | buy  | 130   | 1901   |

    Then the parties should have the following margin levels:
      | party | market id | maintenance | search | initial | release |
      | aux3  | ETH/DEC19 | 750         | 900    | 1125    | 1500    |
      | aux4  | ETH/DEC19 | 1388        | 1665   | 2082    | 2776    |
      | lp    | ETH/DEC19 | 189489      | 227386 | 284233  | 378978  |

    # no transfer in lp account since the existing margin is under release level, and above search level
    Then the parties should have the following account balances:
      | party | asset | market id | margin | general      |
      | aux3  | USD   | ETH/DEC19 | 1050   | 999999998950 |
      | aux4  | USD   | ETH/DEC19 | 2220   | 999999997780 |
      | lp    | USD   | ETH/DEC19 | 295261 | 999999614739 |

    # update the best offer
    When the parties amend the following orders:
      | party | reference | price | size delta | tif     |
      | aux4  | bestOffer | 220   | 0          | TIF_GTC |

    # observe volumes change
    Then the order book should have the following volumes for market "ETH/DEC19":
      | side | price | volume |
      | sell | 230   | 964    |
      | sell | 220   | 10     |
      | buy  | 140   | 10     |
      | buy  | 130   | 1901   |

    Then the parties should have the following margin levels:
      | party | market id | maintenance | search | initial | release |
      | aux3  | ETH/DEC19 | 750         | 900    | 1125    | 1500    |
      | aux4  | ETH/DEC19 | 1388        | 1665   | 2082    | 2776    |
      | lp    | ETH/DEC19 | 142426      | 170911 | 213639  | 284852  |

    # transder in lp account from general to margin account since the existing margin account is above release level
    Then the parties should have the following account balances:
      | party | asset | market id | margin | general      |
      | aux3  | USD   | ETH/DEC19 | 1050   | 999999998950 |
      | aux4  | USD   | ETH/DEC19 | 2220   | 999999997780 |
      | lp    | USD   | ETH/DEC19 | 213639 | 999999696361 |