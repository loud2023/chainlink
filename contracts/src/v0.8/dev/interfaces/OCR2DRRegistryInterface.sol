// SPDX-License-Identifier: MIT
pragma solidity ^0.8.6;

/**
 * @title OCR2DR billing subscription registry interface.
 */
interface OCR2DRRegistryInterface {
  struct RequestBilling {
    address client;
    // a unique subscription ID allocated by billing system,
    uint64 subscriptionId;
    // customer specified gas limit for the fulfillment callback
    uint32 gasLimit;
  }

  /**
   * @notice Get configuration relevant for making requests
   * @return uint32 global max for request gas limit
   * @return address[] list of registered DONs
   */
  function getRequestConfig() external view returns (uint32, address[] memory);

  /**
   * @notice Determine the charged fee that will be paid to the Registry owner
   * @param data Encoded OCR2DR request data, use OCR2DRClient API to encode a request
   * @param billing The request's billing configuration
   * @return fee Cost in Juels (1e18) of LINK
   */
  function getRequiredFee(bytes calldata data, OCR2DRRegistryInterface.RequestBilling calldata billing)
    external
    returns (uint96);

  /**
   * @notice Estimate the execution cost in gas that will be reimbursed to the Node Operator who transmits the data on-chain
   * @param billing The request's billing configuration
   * @return gasAmount
   */
  function estimateExecutionGas(OCR2DRRegistryInterface.RequestBilling calldata billing) external returns (uint256);

  /**
   * @notice Estimate the total cost to make a request: gas re-imbursement, plus DON fee, plus Registry fee
   * @param data Encoded OCR2DR request data, use OCR2DRClient API to encode a request
   * @param billing The request's billing configuration
   * @param donRequiredFee Fee charged by the DON that is paid to Oracle Node
   * @return billedCost Cost in Juels (1e18) of LINK
   */
  function estimateCost(
    bytes calldata data,
    OCR2DRRegistryInterface.RequestBilling calldata billing,
    uint96 donRequiredFee
  ) external view returns (uint96);

  /**
   * @notice Initiate the billing process for an OCR2DR request
   * @param data Encoded OCR2DR request data, use OCR2DRClient API to encode a request
   * @param billing Billing configuration for the request
   * @return requestId - A unique identifier of the request. Can be used to match a request to a response in fulfillRequest.
   * @dev Only callable by OCR2DROracles that have been approved on the Registry
   */
  function beginBilling(bytes calldata data, RequestBilling calldata billing) external returns (bytes32);

  /**
   * @notice Finalize billing process for an OCR2DR request
   * @param requestId contains the proof and response
   * @param response response data from DON consensus
   * @param err error from DON consensus
   * @param transmitter the Oracle who sent the report
   * @param signers the Oracles who signed the report
   * @param initialGas the initial amount of gas that was sent by the transmitter when submitting the report
   * @dev Only callable by OCR2DROracles that have been approved on the Registry
   * @dev simulated offchain to determine if sufficient balance is present to fulfill the request
   */
  function concludeBilling(
    bytes32 requestId,
    bytes calldata response,
    bytes calldata err,
    address transmitter,
    address[31] memory signers, // Matches maxNumOracles from OCR2Abstract.sol
    uint32 initialGas
  ) external returns (uint96);

  /**
   * @notice Get request commitment
   * @param requestId id of request
   * @dev used to determine if a request is fulfilled or not
   */
  function getCommitment(bytes32 requestId)
    external
    view
    returns (
      address,
      uint64,
      uint32
    );

  /**
   * @notice Create a new subscription.
   * @return subId - A unique subscription id.
   * @dev You can manage the consumer set dynamically with addConsumer/removeConsumer.
   * @dev Note to fund the subscription, use transferAndCall. For example
   * @dev  LINKTOKEN.transferAndCall(
   * @dev    address(REGISTRY),
   * @dev    amount,
   * @dev    abi.encode(subId));
   */
  function createSubscription() external returns (uint64 subId);

  /**
   * @notice Get details about a subscription.
   * @param subId - ID of the subscription
   * @return balance - LINK balance of the subscription in juels.
   * @return owner - owner of the subscription.
   * @return consumers - list of consumer address which are able to use this subscription.
   */
  function getSubscription(uint64 subId)
    external
    view
    returns (
      uint96 balance,
      address owner,
      address[] memory consumers
    );

  /**
   * @notice Request subscription owner transfer.
   * @param subId - ID of the subscription
   * @param newOwner - proposed new owner of the subscription
   */
  function requestSubscriptionOwnerTransfer(uint64 subId, address newOwner) external;

  /**
   * @notice Request subscription owner transfer.
   * @param subId - ID of the subscription
   * @dev will revert if original owner of subId has
   * not requested that msg.sender become the new owner.
   */
  function acceptSubscriptionOwnerTransfer(uint64 subId) external;

  /**
   * @notice Add a consumer to a OCR2DR subscription.
   * @param subId - ID of the subscription
   * @param consumer - New consumer which can use the subscription
   */
  function addConsumer(uint64 subId, address consumer) external;

  /**
   * @notice Remove a consumer from a OCR2DR subscription.
   * @param subId - ID of the subscription
   * @param consumer - Consumer to remove from the subscription
   */
  function removeConsumer(uint64 subId, address consumer) external;

  /**
   * @notice Cancel a subscription
   * @param subId - ID of the subscription
   * @param to - Where to send the remaining LINK to
   */
  function cancelSubscription(uint64 subId, address to) external;

  /*
   * @notice Check to see if there exists a request commitment consumers
   * for all consumers and keyhashes for a given sub.
   * @param subId - ID of the subscription
   * @return true if there exists at least one unfulfilled request for the subscription, false
   * otherwise.
   */
  function pendingRequestExists(uint64 subId) external view returns (bool);

  /*
   * @notice Oracle withdraw LINK earned through fulfilling requests
   * @param recipient where to send the funds
   * @param amount amount to withdraw
   */
  function oracleWithdraw(address recipient, uint96 amount) external;
}
