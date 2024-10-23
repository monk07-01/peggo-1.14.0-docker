pragma solidity ^0.8.0;
import "./@openzeppelin/contracts/ERC20.sol";

// One of three testing coins
contract TestERC20 is ERC20 {
	constructor() ERC20("TBFH", "TBFH") {
		_mint(0x3298Addd985Ee761FD58490Fc1392dC484D7c9bD, 10000);
		_mint(0x07523437e0e198A5Dd7d2D3981fed66E3580a4A1, 10000);
		_mint(0x2E78a60A868bEB1f1b07F8FF9A7cED6C716e51E7, 10000);
		_mint(0x14F7b67B707721D58d9CeEB67aadB112D99b0499, 10000);
		_mint(0xE8a162fc1026A5948C6E069b02716c911ce1d933, 10000);
		// this is the EtherBase address for our testnet miner in
		// tests/assets/ETHGenesis.json so it wil have both a lot
		// of ETH and a lot of erc20 tokens to test with
		_mint(msg.sender, 100000000000000000000000000);
	}
}
