import { grpc } from '@improbable-eng/grpc-web'
import { Buffer } from 'buffer'
import { Platform } from 'react-native'

import beapi from '@berty/api'
import { Service } from '@berty/grpc-bridge'
import { logger } from '@berty/grpc-bridge/middleware'
import { grpcweb as rpcWeb } from '@berty/grpc-bridge/rpc'
import rpcBridge from '@berty/grpc-bridge/rpc/rpc.bridge'
import { WelshAccountServiceClient } from '@berty/grpc-bridge/welsh-clients.gen'

import { convertMAddr } from '../ipfs/convertMAddr'

const defaultMAddr =
	Platform.OS === 'web' && convertMAddr([window.location.hash.substring(1) || ''])
const opts: grpc.ClientRpcOptions = {
	transport: grpc.CrossBrowserHttpTransport({ withCredentials: false }),
	host: defaultMAddr || '',
}

export const accountService =
	Platform.OS === 'web'
		? (Service(beapi.account.AccountService, rpcWeb(opts)) as unknown as WelshAccountServiceClient)
		: Service(beapi.account.AccountService, rpcBridge, logger.create('ACCOUNT'))

export const storageSet = async (key: string, value: string) => {
	await accountService.appStoragePut({ key, value: Buffer.from(value, 'utf-8'), global: true })
}

export const storageRemove = async (key: string) => {
	await accountService.appStorageRemove({ key, global: true })
}

export const storageGet = async (key: string) => {
	try {
		const reply = await accountService.appStorageGet({ key, global: true })
		return Buffer.from(reply.value).toString('utf-8')
	} catch (e) {
		if ((e as Error).message.includes('datastore: key not found')) {
			return ''
		}
		throw e
	}
}