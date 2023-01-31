import { getUsers, Personas } from '../../test-helpers/setup'
import { ethers } from 'hardhat'
import { Signer } from 'ethers'
import {
  MockAuthorizedForwarder,
  HeartbeatRequester,
  MockAggregatorProxy,
  MockOffchainAggregator,
} from '../../../typechain'
import { HeartbeatRequester__factory as HeartbeatRequesterFactory } from '../../../typechain/factories/HeartbeatRequester__factory'
import { MockAuthorizedForwarder__factory as MockAuthorizedForwarderFactory } from '../../../typechain/factories/MockAuthorizedForwarder__factory'
import { MockAggregatorProxy__factory as MockAggregatorProxyFactory } from '../../../typechain/factories/MockAggregatorProxy__factory'
import { MockOffchainAggregator__factory as MockOffchainAggregatorFactory } from '../../../typechain/factories/MockOffchainAggregator__factory'
import { assert, expect } from 'chai'

let personas: Personas
let owner: Signer
let caller1: Signer
let proxy: Signer
let aggregator: MockOffchainAggregator
let aggregatorFactory: MockOffchainAggregatorFactory
let aggregatorProxy: MockAggregatorProxy
let aggregatorProxyFactory: MockAggregatorProxyFactory
let requester: HeartbeatRequester
let requesterFactory: HeartbeatRequesterFactory
let authorizedForwarder: MockAuthorizedForwarder
let authorizedForwarderFactory: MockAuthorizedForwarderFactory

describe('HeartbeatRequester', () => {
  beforeEach(async () => {
    personas = (await getUsers()).personas
    owner = personas.Default
    caller1 = personas.Carol
    proxy = personas.Nelly

    // deploy authorized forwarder
    authorizedForwarderFactory = await ethers.getContractFactory(
      'MockAuthorizedForwarder',
    )
    authorizedForwarder = await authorizedForwarderFactory
      .connect(owner)
      .deploy()
    await authorizedForwarder.deployed()

    // deploy heartbeat requester
    requesterFactory = await ethers.getContractFactory('HeartbeatRequester')
    requester = await requesterFactory.connect(owner).deploy()
    await requester.deployed()
  })

  describe('#permitHeartbeat', () => {
    it('adds a heartbeat and emits an event', async () => {
      const callerAddress = await caller1.getAddress()
      const proxyAddress = await proxy.getAddress()
      const tx = await requester
        .connect(owner)
        .permitHeartbeat(callerAddress, proxyAddress)
      await expect(tx)
        .to.emit(requester, 'HeartbeatPermitted')
        .withArgs(callerAddress, proxyAddress)
    })

    it('reverts when not called by its owner', async () => {
      const callerAddress = await caller1.getAddress()
      const proxyAddress = await proxy.getAddress()
      await expect(
        requester.connect(caller1).permitHeartbeat(callerAddress, proxyAddress),
      ).to.be.revertedWith('Only callable by owner')
    })
  })

  describe('#removeHeartbeat', () => {
    it('removes a heartbeat and emits an event', async () => {
      const callerAddress = await caller1.getAddress()
      const proxyAddress = await proxy.getAddress()
      const tx1 = await requester
        .connect(owner)
        .permitHeartbeat(callerAddress, proxyAddress)
      await expect(tx1)
        .to.emit(requester, 'HeartbeatPermitted')
        .withArgs(callerAddress, proxyAddress)

      const tx2 = await requester.connect(owner).removeHeartbeat(callerAddress)
      await expect(tx2)
        .to.emit(requester, 'HeartbeatRemoved')
        .withArgs(callerAddress)
    })

    it('reverts when not called by its owner', async () => {
      await expect(
        requester.connect(caller1).removeHeartbeat(await caller1.getAddress()),
      ).to.be.revertedWith('Only callable by owner')
    })
  })

  describe('#getAggregatorAndRequestHeartbeat', () => {
    it('reverts if caller and proxy combination is not allowed', async () => {
      const callerAddress = await caller1.getAddress()
      const proxyAddress = await proxy.getAddress()
      await requester
        .connect(owner)
        .permitHeartbeat(callerAddress, proxyAddress)

      await expect(
        requester
          .connect(caller1)
          .getAggregatorAndForward(
            await owner.getAddress(),
            authorizedForwarder.address,
          ),
      ).to.be.revertedWith('HeartbeatNotPermitted()')
    })

    it('calls corresponding aggregator to request a new round', async () => {
      aggregatorFactory = await ethers.getContractFactory(
        'MockOffchainAggregator',
      )
      aggregator = await aggregatorFactory.connect(owner).deploy()
      await aggregator.deployed()

      aggregatorProxyFactory = await ethers.getContractFactory(
        'MockAggregatorProxy',
      )
      aggregatorProxy = await aggregatorProxyFactory
        .connect(owner)
        .deploy(aggregator.address)
      await aggregatorProxy.deployed()

      await requester
        .connect(owner)
        .permitHeartbeat(await caller1.getAddress(), aggregatorProxy.address)

      const ABI = ['function requestNewRound()']
      const i = new ethers.utils.Interface(ABI)
      const calldata = i.encodeFunctionData('requestNewRound', [])
      const tx1 = await requester
        .connect(caller1)
        .getAggregatorAndForward(
          aggregatorProxy.address,
          authorizedForwarder.address,
        )

      await expect(tx1)
        .to.emit(authorizedForwarder, 'ForwardFuncCalled')
        .withArgs(aggregator.address, calldata)
      await expect(tx1).to.emit(aggregator, 'RoundIdUpdated').withArgs(1)
      assert.equal((await aggregator.roundId()).toNumber(), 1)

      const tx2 = await requester
        .connect(caller1)
        .getAggregatorAndForward(
          aggregatorProxy.address,
          authorizedForwarder.address,
        )

      await expect(tx2)
        .to.emit(authorizedForwarder, 'ForwardFuncCalled')
        .withArgs(aggregator.address, calldata)
      await expect(tx2).to.emit(aggregator, 'RoundIdUpdated').withArgs(2)
      assert.equal((await aggregator.roundId()).toNumber(), 2)
    })
  })
})