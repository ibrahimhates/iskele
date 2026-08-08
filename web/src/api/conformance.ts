// Compile-time proof that the hand-written wire types in ./types match the
// OpenAPI document.
//
// ./types is what the application reads: it is shorter, it names unions the
// UI cares about, and it does not change shape when the generator does.
// ./schema.d.ts is `make gen-api` output, regenerated from docs/openapi.yaml.
// Neither is authoritative over the other by itself — this file is what makes
// a divergence between them fail `npm run build` instead of shipping.
//
// There is no runtime code here; every declaration below is erased.

import type { components } from './schema';
import type {
  BatchResponse,
  BatchResult,
  Container,
  ContainerDetail,
  DiskUsage,
  DockerEvent,
  Image,
  LogFrame,
  NetworkResource,
  RedeployResult,
  Session,
  Stats,
  SystemInfo,
  User,
  Volume,
} from './types';

type Schemas = components['schemas'];

/**
 * Fails to compile unless `T` can stand in for the generated `S` everywhere.
 *
 * Assignability, not equality: a hand-written type may narrow a `string` to a
 * union (`state`, `role`) or leave out a field the UI never reads. What it may
 * not do is require a field the server does not send, or type one differently
 * from the way the server sends it.
 */
type Conforms<T extends S, S> = [T, S];

export type ContainerConforms = Conforms<Container, Schemas['Container']>;
export type ContainerDetailConforms = Conforms<ContainerDetail, Schemas['ContainerDetail']>;
export type ImageConforms = Conforms<Image, Schemas['Image']>;
export type VolumeConforms = Conforms<Volume, Schemas['Volume']>;
export type NetworkConforms = Conforms<NetworkResource, Schemas['Network']>;
export type SystemInfoConforms = Conforms<SystemInfo, Schemas['SystemInfo']>;
export type DiskUsageConforms = Conforms<DiskUsage, Schemas['DiskUsage']>;
export type SessionConforms = Conforms<Session, Schemas['Session']>;
export type UserConforms = Conforms<User, Schemas['User']>;
export type BatchResultConforms = Conforms<BatchResult, Schemas['BatchResult']>;
export type BatchResponseConforms = Conforms<BatchResponse, Schemas['BatchResponse']>;
export type RedeployResultConforms = Conforms<RedeployResult, Schemas['RedeployResult']>;
export type StatsConforms = Conforms<Stats, Schemas['Stats']>;
export type LogFrameConforms = Conforms<LogFrame, Schemas['LogFrame']>;
export type DockerEventConforms = Conforms<DockerEvent, Schemas['DockerEvent']>;
