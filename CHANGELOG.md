# Changelog

## 0.23.1

*2020-08-27*

This release backports a fix from the forthcoming 0.24.0 release that fixes a GraphQL issue with the new `Asset` type. When fetching the Assets from the top level, all the details came through. When fetching them as a nested property, only the ID was filled in. This is now fixed.

## Improvements

- [#2140](https://github.com/vegaprotocol/vega/pull/2140) GraphQL fix for fetching assets as nested properties

## 0.23.0

*2020-08-10*

This release contains a lot of groundwork for Fees and Auction mode.

**Fees** are incurred on every trade on Vega. Those fees are divided between up to three recipient types, but traders will only see one collective fee charged. The fees reward liquidity providers, infrastructure providers and market makers.

* The liquidity portion of the fee is paid to market makers for providing liquidity, and is transferred to the market-maker fee pool for the market.
* The infrastructure portion of the fee, which is paid to validators as a reward for running the infrastructure of the network, is transferred to the infrastructure fee pool for that asset. It is then periodically distributed to the validators.
* The maker portion of the fee is transferred to the non-aggressive, or passive party in the trade (the maker, as opposed to the taker).

**Auction mode** is not enabled in this release, but the work is nearly complete for Opening Auctions on new markets.

💥 Please note, **this release disables order amends**. The team uncovered an issue in the Market Depth API output that is caused by order amends, so rather than give incorrect output, we've temporarily disabled the amendment of orders. They will return when the Market Depth API is fixed. For now, *amends will return an error*.

### New

- [#2092](https://github.com/vegaprotocol/vega/issues/2092) 💥 Disable order amends
- [#2027](https://github.com/vegaprotocol/vega/pull/2027) Add built in asset faucet endpoint
- [#2075](https://github.com/vegaprotocol/vega/pull/2075), [#2086](https://github.com/vegaprotocol/vega/pull/2086), [#2083](https://github.com/vegaprotocol/vega/pull/2083), [#2078](https://github.com/vegaprotocol/vega/pull/2078) Add time & size limits to faucet requests
- [#2068](https://github.com/vegaprotocol/vega/pull/2068) Add REST endpoint to fetch governance proposals by Party
- [#2058](https://github.com/vegaprotocol/vega/pull/2058) Add REST endpoints for fees
- [#2047](https://github.com/vegaprotocol/vega/pull/2047) Add `prepareWithdraw` endpoint

### Improvements

- [#2061](https://github.com/vegaprotocol/vega/pull/2061) Fix Network orders being left as active
- [#2034](https://github.com/vegaprotocol/vega/pull/2034) Send `KeepAlive` messages on GraphQL subscriptions
- [#2031](https://github.com/vegaprotocol/vega/pull/2031) Add proto fields required for auctions
- [#2025](https://github.com/vegaprotocol/vega/pull/2025) Add auction mode (currently never triggered)
- [#2013](https://github.com/vegaprotocol/vega/pull/2013) Add Opening Auctions support to market framework
- [#2010](https://github.com/vegaprotocol/vega/pull/2010) Add documentation for Order Errors to proto source files
- [#2003](https://github.com/vegaprotocol/vega/pull/2003) Add fees support
- [#2004](https://github.com/vegaprotocol/vega/pull/2004) Remove @deprecated field from GraphQL input types (as it’s invalid)
- [#2000](https://github.com/vegaprotocol/vega/pull/2000) Fix `rejectionReason` for trades stopped for self trading
- [#1990](https://github.com/vegaprotocol/vega/pull/1990) Remove specified `tickSize` from market
- [#2066](https://github.com/vegaprotocol/vega/pull/2066) Fix validation of proposal timestamps to ensure that datestamps specify events in the correct order
- [#2043](https://github.com/vegaprotocol/vega/pull/2043) Track Event Queue events to avoid processing events from other chains twice
## 0.22.0

### Bugfixes
- [#2096](https://github.com/vegaprotocol/vega/pull/2096) Fix concurrent map access in event forward

*2020-07-20*

This release primarily focuses on setting up Vega nodes to deal correctly with events sourced from other chains, working towards bridging assets from Ethereum. This includes responding to asset events from Ethereum, and support for validator nodes notarising asset movements and proposals.

It also contains a lot of bug fixes and improvements, primarily around an internal refactor to using an event bus to communicate between packages. Also included are some corrections for order statuses that were incorrectly being reported or left outdated on the APIs.

### New

- [#1825](https://github.com/vegaprotocol/vega/pull/1825) Add new Notary package for tracking multisig decisions for governance
- [#1837](https://github.com/vegaprotocol/vega/pull/1837) Add support for two-step governance processes such as asset proposals
- [#1856](https://github.com/vegaprotocol/vega/pull/1856) Implement handling of external chain events from the Event Queue
- [#1927](https://github.com/vegaprotocol/vega/pull/1927) Support ERC20 deposits
- [#1987](https://github.com/vegaprotocol/vega/pull/1987) Add `OpenInterest` field to markets
- [#1949](https://github.com/vegaprotocol/vega/pull/1949) Add `RejectionReason` field to rejected governance proposals

### Improvements
- 💥 [#1988](https://github.com/vegaprotocol/vega/pull/1988) REST: Update orders endpoints to use POST, not PUT or DELETE
- 💥 [#1957](https://github.com/vegaprotocol/vega/pull/1957) GraphQL: Some endpoints returned a nullable array of Strings. Now they return an array of nullable strings
- 💥 [#1928](https://github.com/vegaprotocol/vega/pull/1928) GraphQL & GRPC: Remove broken `open` parameter from Orders endpoints. It returned ambiguous results
- 💥 [#1858](https://github.com/vegaprotocol/vega/pull/1858) Fix outdated order details for orders amended by cancel-and-replace
- 💥 [#1849](https://github.com/vegaprotocol/vega/pull/1849) Fix incorrect status on partially filled trades that would have matched with another order by the same user. Was `stopped`, now `rejected`
- 💥 [#1883](https://github.com/vegaprotocol/vega/pull/1883) REST & GraphQL: Market name is now based on the instrument name rather than being set separately
- [#1699](https://github.com/vegaprotocol/vega/pull/1699) Migrate Margin package to event bus
- [#1853](https://github.com/vegaprotocol/vega/pull/1853) Migrate Market package to event bus
- [#1844](https://github.com/vegaprotocol/vega/pull/1844) Migrate Governance package to event
- [#1877](https://github.com/vegaprotocol/vega/pull/1877) Migrate Position package to event
- [#1838](https://github.com/vegaprotocol/vega/pull/1838) GraphQL: Orders now include their `version` and `updatedAt`, which are useful when dealing with amended orders
- [#1841](https://github.com/vegaprotocol/vega/pull/1841) Fix: `expiresAt` on orders was validated at submission time, this has been moved to post-chain validation
- [#1849](https://github.com/vegaprotocol/vega/pull/1849) Improve Order documentation for `Status` and `TimeInForce`
- [#1861](https://github.com/vegaprotocol/vega/pull/1861) Remove single mutex in event bus
- [#1866](https://github.com/vegaprotocol/vega/pull/1866) Add mutexes for event bus access
- [#1889](https://github.com/vegaprotocol/vega/pull/1889) Improve event broker performance
- [#1891](https://github.com/vegaprotocol/vega/pull/1891) Fix context for event subscribers
- [#1889](https://github.com/vegaprotocol/vega/pull/1889) Address event bus performance issues
- [#1892](https://github.com/vegaprotocol/vega/pull/1892) Improve handling for new chain connection proposal
- [#1903](https://github.com/vegaprotocol/vega/pull/1903) Fix regressions in Candles API introduced by event bus
- [#1940](https://github.com/vegaprotocol/vega/pull/1940) Add new asset proposals to GraphQL API
- [#1943](https://github.com/vegaprotocol/vega/pull/1943) Validate list of allowed assets

## 0.21.0

*2020-06-18*

A follow-on from 0.20.1, this release includes a fix for the GraphQL API returning inconsistent values for the `side` field on orders, leading to Vega Console failing to submit orders. As a bonus there is another GraphQL improvement, and two fixes that return more correct values for filled network orders and expired orders.

### Improvements

- 💥 [#1820](https://github.com/vegaprotocol/vega/pull/1820) GraphQL: Non existent parties no longer return a GraphQL error
- 💥 [#1784](https://github.com/vegaprotocol/vega/pull/1784) GraphQL: Update schema and fix enum mappings from Proto
- 💥 [#1761](https://github.com/vegaprotocol/vega/pull/1761) Governance: Improve processing of Proposals
- [#1822](https://github.com/vegaprotocol/vega/pull/1822) Remove duplicate updates to `createdAt`
- [#1818](https://github.com/vegaprotocol/vega/pull/1818) Trades: Replace buffer with events
- [#1812](https://github.com/vegaprotocol/vega/pull/1812) Governance: Improve logging
- [#1810](https://github.com/vegaprotocol/vega/pull/1810) Execution: Set order status for fully filled network orders to be `FILLED`
- [#1803](https://github.com/vegaprotocol/vega/pull/1803) Matching: Set `updatedAt` when orders expire
- [#1780](https://github.com/vegaprotocol/vega/pull/1780) APIs: Reject `NETWORK` orders
- [#1792](https://github.com/vegaprotocol/vega/pull/1792) Update Golang to 1.14 and tendermint to 0.33.5

## 0.20.1

*2020-06-18*

This release fixes one small bug that was causing many closed streams, which was a problem for API clients.

## Improvements

- [#1813](https://github.com/vegaprotocol/vega/pull/1813) Set `PartyEvent` type to party event

## 0.20.0

*2020-06-15*

This release contains a lot of fixes to APIs, and a minor new addition to the statistics endpoint. Potentially breaking changes are now labelled with 💥. If you have implemented a client that fetches candles, places orders or amends orders, please check below.

### Features
- [#1730](https://github.com/vegaprotocol/vega/pull/1730) `ChainID` added to statistics endpoint
- 💥 [#1734](https://github.com/vegaprotocol/vega/pull/1734) Start adding `TraceID` to core events

### Improvements
- 💥 [#1721](https://github.com/vegaprotocol/vega/pull/1721) Improve API responses for `GetProposalById`
- 💥 [#1724](https://github.com/vegaprotocol/vega/pull/1724) New Order: Type no longer defaults to LIMIT orders
- 💥 [#1728](https://github.com/vegaprotocol/vega/pull/1728) `PrepareAmend` no longer accepts expiry time
- 💥 [#1760](https://github.com/vegaprotocol/vega/pull/1760) Add proto enum zero value "unspecified" to Side
- 💥 [#1764](https://github.com/vegaprotocol/vega/pull/1764) Candles: Interval no longer defaults to 1 minute
- 💥 [#1773](https://github.com/vegaprotocol/vega/pull/1773) Add proto enum zero value "unspecified" to `Order.Status`
- 💥 [#1776](https://github.com/vegaprotocol/vega/pull/1776) Add prefixes to enums, add proto zero value "unspecified" to `Trade.Type`
- 💥 [#1781](https://github.com/vegaprotocol/vega/pull/1781) Add prefix and UNSPECIFIED to `ChainStatus`, `AccountType`, `TransferType`
- [#1714](https://github.com/vegaprotocol/vega/pull/1714) Extend governance error handling
- [#1726](https://github.com/vegaprotocol/vega/pull/1726) Mark Price was not always correctly updated on a partial fill
- [#1734](https://github.com/vegaprotocol/vega/pull/1734) Feature/1577 hash context propagation
- [#1741](https://github.com/vegaprotocol/vega/pull/1741) Fix incorrect timestamps for proposals retrieved by GraphQL
- [#1743](https://github.com/vegaprotocol/vega/pull/1743) Orders amended to be GTT now return GTT in the response
- [#1745](https://github.com/vegaprotocol/vega/pull/1745) Votes blob is now base64 encoded
- [#1747](https://github.com/vegaprotocol/vega/pull/1747) Markets created from proposals now have the same ID as the proposal that created them
- [#1750](https://github.com/vegaprotocol/vega/pull/1750) Added datetime to governance votes
- [#1751](https://github.com/vegaprotocol/vega/pull/1751) Fix a bug in governance vote counting
- [#1752](https://github.com/vegaprotocol/vega/pull/1752) Fix incorrect validation on new orders
- [#1757](https://github.com/vegaprotocol/vega/pull/1757) Fix incorrect party ID validation on new orders
- [#1758](https://github.com/vegaprotocol/vega/pull/1758) Fix issue where markets created via governance were not tradable
- [#1763](https://github.com/vegaprotocol/vega/pull/1763) Expiration settlement date for market changed to 30/10/2020-22:59:59
- [#1777](https://github.com/vegaprotocol/vega/pull/1777) Create `README.md`
- [#1764](https://github.com/vegaprotocol/vega/pull/1764) Add proto enum zero value "unspecified" to Interval
- [#1767](https://github.com/vegaprotocol/vega/pull/1767) Feature/1692 order event
- [#1787](https://github.com/vegaprotocol/vega/pull/1787) Feature/1697 account event
- [#1788](https://github.com/vegaprotocol/vega/pull/1788) Check for unspecified Vote value
- [#1794](https://github.com/vegaprotocol/vega/pull/1794) Feature/1696 party event

## 0.19.0

*2020-05-26*

This release fixes a handful of bugs, primarily around order amends and new market governance proposals.

### Features

- [#1658](https://github.com/vegaprotocol/vega/pull/1658) Add timestamps to proposal API responses
- [#1656](https://github.com/vegaprotocol/vega/pull/1656) Add margin checks to amends
- [#1679](https://github.com/vegaprotocol/vega/pull/1679) Add topology package to map Validator nodes to Vega keypairs

### Improvements
- [#1718](https://github.com/vegaprotocol/vega/pull/1718) Fix a case where a party can cancel another party's orders
- [#1662](https://github.com/vegaprotocol/vega/pull/1662) Start moving to event-based architecture internally
- [#1684](https://github.com/vegaprotocol/vega/pull/1684) Fix order expiry handling when `expiresAt` is amended
- [#1686](https://github.com/vegaprotocol/vega/pull/1686) Fix participation stake to have a maximum of 100%
- [#1607](https://github.com/vegaprotocol/vega/pull/1607) Update `gqlgen` dependency to 0.11.3
- [#1711](https://github.com/vegaprotocol/vega/pull/1711) Remove ID from market proposal input
- [#1712](https://github.com/vegaprotocol/vega/pull/1712) `prepareProposal` no longer returns an ID on market proposals
- [#1707](https://github.com/vegaprotocol/vega/pull/1707) Allow overriding default governance parameters via `ldflags`.
- [#1715](https://github.com/vegaprotocol/vega/pull/1715) Compile testing binary with short-lived governance periods

## 0.18.1

*2020-05-13*

### Improvements
- [#1649](https://github.com/vegaprotocol/vega/pull/1649)
    Fix github artefact upload CI configuration

## 0.18.0

*2020-05-12*

From this release forward, compiled binaries for multiple platforms will be attached to the release on GitHub.

### Features

- [#1636](https://github.com/vegaprotocol/vega/pull/1636)
    Add a default GraphQL query complexity limit of 5. Currently configured to 17 on testnet to support Console.
- [#1656](https://github.com/vegaprotocol/vega/pull/1656)
    Add GraphQL queries for governance proposals
- [#1596](https://github.com/vegaprotocol/vega/pull/1596)
    Add builds for multiple architectures to GitHub releases

### Improvements
- [#1630](https://github.com/vegaprotocol/vega/pull/1630)
    Fix amends triggering multiple updates to the same order
- [#1564](https://github.com/vegaprotocol/vega/pull/1564)
    Hex encode keys

## 0.17.0

*2020-04-21*

### Features

- [#1458](https://github.com/vegaprotocol/vega/issues/1458) Add root GraphQL Orders query.
- [#1457](https://github.com/vegaprotocol/vega/issues/1457) Add GraphQL query to list all known parties.
- [#1455](https://github.com/vegaprotocol/vega/issues/1455) Remove party list from stats endpoint.
- [#1448](https://github.com/vegaprotocol/vega/issues/1448) Add `updatedAt` field to orders.

### Improvements

- [#1102](https://github.com/vegaprotocol/vega/issues/1102) Return full Market details in nested GraphQL queries.
- [#1466](https://github.com/vegaprotocol/vega/issues/1466) Flush orders before trades. This fixes a rare scenario where a trade can be available through the API, but not the order that triggered it.
- [#1491](https://github.com/vegaprotocol/vega/issues/1491) Fix `OrdersByMarket` and `OrdersByParty` 'Open' parameter.
- [#1472](https://github.com/vegaprotocol/vega/issues/1472) Fix Orders by the same party matching.

### Upcoming changes

This release contains the initial partial implementation of Governance. This will be finished and documented in 0.18.0.

## 0.16.2

*2020-04-16*

### Improvements

- [#1545](https://github.com/vegaprotocol/vega/pull/1545) Improve error handling in `Prepare*Order` requests

## 0.16.1

*2020-04-15*

### Improvements

- [!651](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/651) Prevent bad ED25519 key length causing node panic.

## 0.16.0

*2020-03-02*

### Features

- The new authentication service is in place. The existing authentication service is now deprecated and will be removed in the next release.

### Improvements

- [!609](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/609) Show trades resulting from Orders created by the network (for example close outs) in the API.
- [!604](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/604) Add `lastMarketPrice` settlement.
- [!614](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/614) Fix casing of Order parameter `timeInForce`.
- [!615](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/615) Add new order statuses, `Rejected` and `PartiallyFilled`.
- [!622](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/622) GraphQL: Change Buyer and Seller properties on Trades from string to Party.
- [!599](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/599) Pin Market IDs to fixed values.
- [!603](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/603), [!611](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/611) Remove `NotifyTraderAccount` from API documentation.
- [!624](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/624) Add protobuf validators to API requests.
- [!595](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/595), [!621](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/621), [!623](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/623) Fix a flaky integration test.
- [!601](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/601) Improve matching engine coverage.
- [!612](https://gitlab.com/vega-protocol/trading-core/-/merge_requests/612) Improve collateral engine test coverage.
