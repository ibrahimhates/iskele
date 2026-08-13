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
  AuditEntry,
  AuditFacets,
  AuditPage,
  BatchResponse,
  CreateResult,
  BatchResult,
  Build,
  BuildFrame,
  Container,
  ContainerDetail,
  DirEntry,
  DirListing,
  DiscoveredStack,
  DaemonInfo,
  DiskUsage,
  DockerEvent,
  EngineSummary,
  HostCPU,
  HostDisk,
  HostLoad,
  HostMemory,
  HostReport,
  Image,
  LogFrame,
  NetworkResource,
  RedeployResult,
  Session,
  Stack,
  StackActionResult,
  StackDiff,
  StackEvent,
  StackValidation,
  SettingsUpdate,
  SettingsView,
  Stats,
  SystemInfo,
  TOTPSetup,
  Template,
  TemplateField,
  TemplateProblem,
  User,
  UserCreate,
  UserUpdate,
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
export type SettingsViewConforms = Conforms<SettingsView, Schemas['SettingsView']>;
export type SettingsUpdateConforms = Conforms<SettingsUpdate, Schemas['SettingsUpdate']>;
export type AuditEntryConforms = Conforms<AuditEntry, Schemas['AuditEntry']>;
export type AuditPageConforms = Conforms<AuditPage, Schemas['AuditPage']>;
export type AuditFacetsConforms = Conforms<AuditFacets, Schemas['AuditFacets']>;
export type DiskUsageConforms = Conforms<DiskUsage, Schemas['DiskUsage']>;
export type HostReportConforms = Conforms<HostReport, Schemas['HostReport']>;
export type HostCPUConforms = Conforms<HostCPU, Schemas['HostCPU']>;
export type HostMemoryConforms = Conforms<HostMemory, Schemas['HostMemory']>;
export type HostDiskConforms = Conforms<HostDisk, Schemas['HostDisk']>;
export type HostLoadConforms = Conforms<HostLoad, Schemas['HostLoad']>;
export type DaemonInfoConforms = Conforms<DaemonInfo, Schemas['DaemonInfo']>;
export type EngineSummaryConforms = Conforms<EngineSummary, Schemas['EngineSummary']>;
export type SessionConforms = Conforms<Session, Schemas['Session']>;
export type UserConforms = Conforms<User, Schemas['User']>;
export type UserCreateConforms = Conforms<UserCreate, Schemas['UserCreate']>;
export type UserUpdateConforms = Conforms<UserUpdate, Schemas['UserUpdate']>;
export type TOTPSetupConforms = Conforms<TOTPSetup, Schemas['TOTPSetup']>;
export type BatchResultConforms = Conforms<BatchResult, Schemas['BatchResult']>;
export type BatchResponseConforms = Conforms<BatchResponse, Schemas['BatchResponse']>;
export type RedeployResultConforms = Conforms<RedeployResult, Schemas['RedeployResult']>;
export type StatsConforms = Conforms<Stats, Schemas['Stats']>;
export type LogFrameConforms = Conforms<LogFrame, Schemas['LogFrame']>;
export type DockerEventConforms = Conforms<DockerEvent, Schemas['DockerEvent']>;
export type DirEntryConforms = Conforms<DirEntry, Schemas['DirEntry']>;
export type DirListingConforms = Conforms<DirListing, Schemas['DirListing']>;
export type BuildConforms = Conforms<Build, Schemas['Build']>;
export type BuildFrameConforms = Conforms<BuildFrame, Schemas['BuildFrame']>;
export type StackConforms = Conforms<Stack, Schemas['Stack']>;
export type StackValidationConforms = Conforms<StackValidation, Schemas['StackValidation']>;
export type StackDiffConforms = Conforms<StackDiff, Schemas['StackDiff']>;
export type StackEventConforms = Conforms<StackEvent, Schemas['StackEvent']>;
export type StackActionResultConforms = Conforms<StackActionResult, Schemas['StackActionResult']>;
export type DiscoveredStackConforms = Conforms<DiscoveredStack, Schemas['DiscoveredStack']>;
export type TemplateConforms = Conforms<Template, Schemas['Template']>;
export type TemplateFieldConforms = Conforms<TemplateField, Schemas['TemplateField']>;
export type TemplateProblemConforms = Conforms<TemplateProblem, Schemas['TemplateProblem']>;
export type CreateResultConforms = Conforms<CreateResult, Schemas['CreateResult']>;
