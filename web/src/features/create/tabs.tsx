import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import type { FormState, EnvRow, MountRow, PairRow, PortRow } from './state';
import { bindSources, newRowID, parseDotEnv } from './state';
import type { PullPolicy, RestartPolicyName } from '../../api/types';
import { Field, PrivilegedNotice, RowList, Section, Toggle } from './fields';

/** What every tab needs to read and write the form. */
export interface TabProps {
  form: FormState;
  set: <K extends keyof FormState>(key: K, value: FormState[K]) => void;
  /** Whether the caller holds the privileged permission. */
  canPrivileged: boolean;
  /** The host paths bind mounts may use. */
  allowedPaths: string[];
  /** Networks that exist, for the picker. */
  networks: string[];
}

export function GeneralTab({ form, set }: TabProps) {
  const { t } = useTranslation();

  return (
    <div className="space-y-5">
      <Field label={t('create.image')} hint={t('create.image_hint')} htmlFor="image">
        <input
          id="image"
          className="input font-mono"
          value={form.image}
          placeholder="nginx:1.27"
          autoFocus
          onChange={(e) => set('image', e.target.value)}
        />
      </Field>

      <Field label={t('create.name')} hint={t('create.name_hint')} htmlFor="name">
        <input
          id="name"
          className="input"
          value={form.name}
          placeholder="web"
          onChange={(e) => set('name', e.target.value)}
        />
      </Field>

      <Field label={t('create.pull_policy')} htmlFor="pull">
        <select
          id="pull"
          className="input"
          value={form.pullPolicy}
          onChange={(e) => set('pullPolicy', e.target.value as PullPolicy)}
        >
          <option value="missing">{t('create.pull_missing')}</option>
          <option value="always">{t('create.pull_always')}</option>
          <option value="never">{t('create.pull_never')}</option>
        </select>
      </Field>

      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t('create.restart_policy')} htmlFor="restart">
          <select
            id="restart"
            className="input"
            value={form.restartPolicy}
            onChange={(e) => set('restartPolicy', e.target.value as RestartPolicyName)}
          >
            <option value="no">no</option>
            <option value="on-failure">on-failure</option>
            <option value="always">always</option>
            <option value="unless-stopped">unless-stopped</option>
          </select>
        </Field>

        {form.restartPolicy === 'on-failure' && (
          <Field label={t('create.max_retries')} htmlFor="retries">
            <input
              id="retries"
              type="number"
              min={0}
              className="input"
              value={form.maxRetries}
              onChange={(e) => set('maxRetries', e.target.value)}
            />
          </Field>
        )}
      </div>

      <div className="space-y-2">
        <Toggle
          label={t('create.start_now')}
          checked={form.start}
          onChange={(v) => set('start', v)}
        />
        <Toggle
          label={t('create.auto_remove')}
          checked={form.autoRemove}
          onChange={(v) => set('autoRemove', v)}
        />
        <Toggle label={t('create.init')} checked={form.init} onChange={(v) => set('init', v)} />
      </div>
    </div>
  );
}

export function CommandTab({ form, set }: TabProps) {
  const { t } = useTranslation();

  return (
    <div className="space-y-5">
      <Field label={t('create.command_label')} hint={t('create.command_hint')} htmlFor="command">
        <input
          id="command"
          className="input font-mono"
          value={form.command}
          placeholder="nginx -g 'daemon off;'"
          onChange={(e) => set('command', e.target.value)}
        />
      </Field>

      <Field label={t('create.entrypoint')} htmlFor="entrypoint">
        <input
          id="entrypoint"
          className="input font-mono"
          value={form.entrypoint}
          onChange={(e) => set('entrypoint', e.target.value)}
        />
      </Field>

      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t('create.working_dir')} htmlFor="workdir">
          <input
            id="workdir"
            className="input font-mono"
            value={form.workingDir}
            placeholder="/app"
            onChange={(e) => set('workingDir', e.target.value)}
          />
        </Field>

        <Field label={t('create.user')} hint={t('create.user_hint')} htmlFor="user">
          <input
            id="user"
            className="input font-mono"
            value={form.user}
            placeholder="1000:1000"
            onChange={(e) => set('user', e.target.value)}
          />
        </Field>
      </div>

      <Field label={t('create.hostname')} htmlFor="hostname">
        <input
          id="hostname"
          className="input"
          value={form.hostname}
          onChange={(e) => set('hostname', e.target.value)}
        />
      </Field>

      <div className="space-y-2">
        <Toggle label={t('create.tty')} checked={form.tty} onChange={(v) => set('tty', v)} />
        <Toggle
          label={t('create.stdin')}
          checked={form.openStdin}
          onChange={(v) => set('openStdin', v)}
        />
      </div>
    </div>
  );
}

export function PortsTab({ form, set }: TabProps) {
  const { t } = useTranslation();

  function update(id: number, patch: Partial<PortRow>) {
    set(
      'ports',
      form.ports.map((row) => (row.id === id ? { ...row, ...patch } : row)),
    );
  }

  return (
    <div className="space-y-4">
      <RowList
        rows={form.ports}
        columns={[
          t('create.host_ip'),
          t('create.host_port'),
          t('create.container_port'),
          t('create.protocol'),
        ]}
        emptyLabel={t('create.ports_empty')}
        addLabel={t('create.add_port')}
        onAdd={() => set('ports', [...form.ports, { id: newRowID(), container_port: 0 }])}
        onRemove={(id) =>
          set(
            'ports',
            form.ports.filter((row) => row.id !== id),
          )
        }
      >
        {(row) => (
          <>
            <input
              className="input flex-1 font-mono"
              value={row.host_ip ?? ''}
              placeholder="127.0.0.1"
              aria-label={t('create.host_ip')}
              onChange={(e) => update(row.id, { host_ip: e.target.value })}
            />
            <input
              className="input flex-1 font-mono"
              value={row.host_port ?? ''}
              placeholder="8080"
              aria-label={t('create.host_port')}
              onChange={(e) => update(row.id, { host_port: e.target.value })}
            />
            <input
              className="input flex-1 font-mono"
              type="number"
              min={1}
              max={65535}
              value={row.container_port || ''}
              placeholder="80"
              aria-label={t('create.container_port')}
              onChange={(e) => update(row.id, { container_port: Number(e.target.value) })}
            />
            <select
              className="input flex-1"
              value={row.protocol ?? 'tcp'}
              aria-label={t('create.protocol')}
              onChange={(e) => update(row.id, { protocol: e.target.value as 'tcp' | 'udp' })}
            >
              <option value="tcp">tcp</option>
              <option value="udp">udp</option>
            </select>
          </>
        )}
      </RowList>

      <p className="text-xs text-muted">{t('create.host_ip_hint')}</p>
    </div>
  );
}

export function VolumesTab({ form, set, allowedPaths }: TabProps) {
  const { t } = useTranslation();

  function update(id: number, patch: Partial<MountRow>) {
    set(
      'mounts',
      form.mounts.map((row) => (row.id === id ? { ...row, ...patch } : row)),
    );
  }

  // The same check the server runs, so the operator is told before submitting
  // rather than after being refused.
  const outside = bindSources(form).filter(
    (source) => !allowedPaths.some((root) => source === root || source.startsWith(`${root}/`)),
  );

  return (
    <div className="space-y-4">
      <p className="rounded-md border border-border bg-elevated px-3 py-2 text-xs text-muted">
        {allowedPaths.length === 0
          ? t('create.allowed_paths_none')
          : t('create.allowed_paths', { paths: allowedPaths.join(', ') })}
      </p>

      {outside.map((path) => (
        <p key={path} className="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-xs">
          {t('create.path_outside', { path })}
        </p>
      ))}

      <RowList
        rows={form.mounts}
        columns={[
          t('create.mount_type'),
          t('create.mount_source'),
          t('create.mount_destination'),
          '',
        ]}
        emptyLabel={t('create.mounts_empty')}
        addLabel={t('create.add_mount')}
        onAdd={() =>
          set('mounts', [...form.mounts, { id: newRowID(), type: 'volume', destination: '' }])
        }
        onRemove={(id) =>
          set(
            'mounts',
            form.mounts.filter((row) => row.id !== id),
          )
        }
      >
        {(row) => (
          <>
            <select
              className="input flex-1"
              value={row.type}
              aria-label={t('create.mount_type')}
              onChange={(e) =>
                update(row.id, { type: e.target.value as MountRow['type'], source: '' })
              }
            >
              <option value="volume">volume</option>
              <option value="bind">bind</option>
              <option value="tmpfs">tmpfs</option>
            </select>

            {row.type === 'tmpfs' ? (
              <input
                className="input flex-1 font-mono"
                type="number"
                min={0}
                value={row.tmpfs_size ? row.tmpfs_size / (1024 * 1024) : ''}
                placeholder={t('create.tmpfs_size')}
                aria-label={t('create.tmpfs_size')}
                onChange={(e) =>
                  update(row.id, { tmpfs_size: Number(e.target.value) * 1024 * 1024 })
                }
              />
            ) : (
              <input
                className="input flex-1 font-mono"
                value={row.source ?? ''}
                placeholder={row.type === 'bind' ? (allowedPaths[0] ?? '/srv/data') : 'pgdata'}
                aria-label={t('create.mount_source')}
                list={row.type === 'bind' ? 'allowed-paths' : undefined}
                onChange={(e) => update(row.id, { source: e.target.value })}
              />
            )}

            <input
              className="input flex-1 font-mono"
              value={row.destination}
              placeholder="/data"
              aria-label={t('create.mount_destination')}
              onChange={(e) => update(row.id, { destination: e.target.value })}
            />

            <label className="flex flex-1 items-center gap-2 text-xs">
              <input
                type="checkbox"
                className="accent-accent"
                checked={row.read_only ?? false}
                onChange={(e) => update(row.id, { read_only: e.target.checked })}
              />
              {t('create.read_only')}
            </label>
          </>
        )}
      </RowList>

      <datalist id="allowed-paths">
        {allowedPaths.map((path) => (
          <option key={path} value={path} />
        ))}
      </datalist>
    </div>
  );
}

export function EnvTab({ form, set }: TabProps) {
  const { t } = useTranslation();
  const [pasted, setPasted] = useState('');

  function update(id: number, patch: Partial<EnvRow>) {
    set(
      'env',
      form.env.map((row) => (row.id === id ? { ...row, ...patch } : row)),
    );
  }

  function applyPaste() {
    const parsed = parseDotEnv(pasted);
    if (parsed.length === 0) return;
    set('env', [...form.env, ...parsed.map((entry) => ({ id: newRowID(), ...entry }))]);
    setPasted('');
  }

  return (
    <div className="space-y-5">
      <RowList
        rows={form.env}
        columns={[t('create.key'), t('create.value')]}
        emptyLabel={t('create.env_empty')}
        addLabel={t('create.add_env')}
        onAdd={() => set('env', [...form.env, { id: newRowID(), key: '', value: '' }])}
        onRemove={(id) =>
          set(
            'env',
            form.env.filter((row) => row.id !== id),
          )
        }
      >
        {(row) => (
          <>
            <input
              className="input flex-1 font-mono"
              value={row.key}
              placeholder="POSTGRES_USER"
              aria-label={t('create.key')}
              onChange={(e) => update(row.id, { key: e.target.value })}
            />
            <input
              className="input flex-1 font-mono"
              value={row.value}
              aria-label={t('create.value')}
              onChange={(e) => update(row.id, { value: e.target.value })}
            />
          </>
        )}
      </RowList>

      <Section title={t('create.paste_env')} description={t('create.paste_env_hint')}>
        <textarea
          className="input h-28 font-mono text-xs"
          value={pasted}
          placeholder={'POSTGRES_USER=app\nPOSTGRES_PASSWORD=secret'}
          aria-label={t('create.paste_env')}
          onChange={(e) => setPasted(e.target.value)}
        />
        <button
          type="button"
          className="btn-default"
          disabled={parseDotEnv(pasted).length === 0}
          onClick={applyPaste}
        >
          {t('create.paste_env_apply')}
        </button>
      </Section>
    </div>
  );
}

export function NetworkTab({ form, set, networks, canPrivileged }: TabProps) {
  const { t } = useTranslation();
  const isHost = form.networkName.trim().toLowerCase() === 'host';

  return (
    <div className="space-y-5">
      <Field label={t('create.network_name')} htmlFor="network">
        <select
          id="network"
          className="input"
          value={form.networkName}
          onChange={(e) => set('networkName', e.target.value)}
        >
          <option value="">{t('create.network_default')}</option>
          {networks.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
          <option value="host">host</option>
          <option value="none">none</option>
        </select>
      </Field>

      {isHost && (
        <PrivilegedNotice allowed={canPrivileged}>
          {canPrivileged ? t('create.privileged_allowed') : t('create.privileged_denied')}
        </PrivilegedNotice>
      )}

      <Field label={t('create.aliases')} hint={t('create.aliases_hint')} htmlFor="aliases">
        <input
          id="aliases"
          className="input font-mono"
          value={form.aliases}
          placeholder="api, backend"
          onChange={(e) => set('aliases', e.target.value)}
        />
      </Field>

      <Field label={t('create.ipv4')} hint={t('create.ipv4_hint')} htmlFor="ipv4">
        <input
          id="ipv4"
          className="input font-mono"
          value={form.ipv4}
          placeholder="172.30.0.5"
          onChange={(e) => set('ipv4', e.target.value)}
        />
      </Field>

      <Field
        label={t('create.extra_hosts')}
        hint={t('create.extra_hosts_hint')}
        htmlFor="extra-hosts"
      >
        <input
          id="extra-hosts"
          className="input font-mono"
          value={form.extraHosts}
          placeholder="db:10.0.0.5"
          onChange={(e) => set('extraHosts', e.target.value)}
        />
      </Field>

      <Field label={t('create.dns')} htmlFor="dns">
        <input
          id="dns"
          className="input font-mono"
          value={form.dns}
          placeholder="1.1.1.1, 9.9.9.9"
          onChange={(e) => set('dns', e.target.value)}
        />
      </Field>
    </div>
  );
}

/** A reusable key/value editor, used for labels, log options and sysctls. */
function PairEditor({
  rows,
  onChange,
  addLabel,
  emptyLabel,
  keyPlaceholder,
}: {
  rows: PairRow[];
  onChange: (rows: PairRow[]) => void;
  addLabel: string;
  emptyLabel: string;
  keyPlaceholder?: string;
}) {
  const { t } = useTranslation();

  return (
    <RowList
      rows={rows}
      columns={[t('create.key'), t('create.value')]}
      emptyLabel={emptyLabel}
      addLabel={addLabel}
      onAdd={() => onChange([...rows, { id: newRowID(), key: '', value: '' }])}
      onRemove={(id) => onChange(rows.filter((row) => row.id !== id))}
    >
      {(row) => (
        <>
          <input
            className="input flex-1 font-mono"
            value={row.key}
            placeholder={keyPlaceholder}
            aria-label={t('create.key')}
            onChange={(e) =>
              onChange(rows.map((r) => (r.id === row.id ? { ...r, key: e.target.value } : r)))
            }
          />
          <input
            className="input flex-1 font-mono"
            value={row.value}
            aria-label={t('create.value')}
            onChange={(e) =>
              onChange(rows.map((r) => (r.id === row.id ? { ...r, value: e.target.value } : r)))
            }
          />
        </>
      )}
    </RowList>
  );
}

export function LabelsTab({ form, set }: TabProps) {
  const { t } = useTranslation();

  return (
    <PairEditor
      rows={form.labels}
      onChange={(rows) => set('labels', rows)}
      addLabel={t('create.add_label')}
      emptyLabel={t('create.labels_empty')}
      keyPlaceholder="com.example.app"
    />
  );
}

export function ResourcesTab({ form, set }: TabProps) {
  const { t } = useTranslation();

  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <Field label={t('create.cpus')} hint={t('create.cpus_hint')} htmlFor="cpus">
        <input
          id="cpus"
          className="input"
          type="number"
          min={0}
          step="0.1"
          value={form.cpus}
          onChange={(e) => set('cpus', e.target.value)}
        />
      </Field>

      <Field label={t('create.cpu_shares')} hint={t('create.cpu_shares_hint')} htmlFor="shares">
        <input
          id="shares"
          className="input"
          type="number"
          min={0}
          value={form.cpuShares}
          onChange={(e) => set('cpuShares', e.target.value)}
        />
      </Field>

      <Field label={t('create.cpuset')} hint={t('create.cpuset_hint')} htmlFor="cpuset">
        <input
          id="cpuset"
          className="input font-mono"
          value={form.cpusetCpus}
          placeholder="0-3"
          onChange={(e) => set('cpusetCpus', e.target.value)}
        />
      </Field>

      <Field label={t('create.memory')} htmlFor="memory">
        <input
          id="memory"
          className="input"
          type="number"
          min={0}
          value={form.memoryMB}
          placeholder="512"
          onChange={(e) => set('memoryMB', e.target.value)}
        />
      </Field>

      <Field label={t('create.memory_reservation')} htmlFor="reservation">
        <input
          id="reservation"
          className="input"
          type="number"
          min={0}
          value={form.memoryReservationMB}
          onChange={(e) => set('memoryReservationMB', e.target.value)}
        />
      </Field>

      <Field label={t('create.pids_limit')} htmlFor="pids">
        <input
          id="pids"
          className="input"
          type="number"
          min={0}
          value={form.pidsLimit}
          onChange={(e) => set('pidsLimit', e.target.value)}
        />
      </Field>

      <Field label={t('create.shm_size')} htmlFor="shm">
        <input
          id="shm"
          className="input"
          type="number"
          min={0}
          value={form.shmSizeMB}
          onChange={(e) => set('shmSizeMB', e.target.value)}
        />
      </Field>
    </div>
  );
}

export function HealthTab({ form, set }: TabProps) {
  const { t } = useTranslation();

  return (
    <div className="space-y-5">
      <Toggle
        label={t('create.health_enable')}
        checked={form.healthEnabled}
        disabled={form.healthDisable}
        onChange={(v) => set('healthEnabled', v)}
      />

      {form.healthEnabled && !form.healthDisable && (
        <>
          <Field
            label={t('create.health_test')}
            hint={t('create.health_test_hint')}
            htmlFor="health-test"
          >
            <input
              id="health-test"
              className="input font-mono"
              value={form.healthTest}
              placeholder="curl -f localhost/health"
              onChange={(e) => set('healthTest', e.target.value)}
            />
          </Field>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('create.health_interval')} htmlFor="health-interval">
              <input
                id="health-interval"
                className="input font-mono"
                value={form.healthInterval}
                placeholder="30s"
                onChange={(e) => set('healthInterval', e.target.value)}
              />
            </Field>
            <Field label={t('create.health_timeout')} htmlFor="health-timeout">
              <input
                id="health-timeout"
                className="input font-mono"
                value={form.healthTimeout}
                placeholder="5s"
                onChange={(e) => set('healthTimeout', e.target.value)}
              />
            </Field>
            <Field label={t('create.health_start_period')} htmlFor="health-start">
              <input
                id="health-start"
                className="input font-mono"
                value={form.healthStartPeriod}
                placeholder="10s"
                onChange={(e) => set('healthStartPeriod', e.target.value)}
              />
            </Field>
            <Field label={t('create.health_retries')} htmlFor="health-retries">
              <input
                id="health-retries"
                className="input"
                type="number"
                min={0}
                value={form.healthRetries}
                onChange={(e) => set('healthRetries', e.target.value)}
              />
            </Field>
          </div>
        </>
      )}

      <Toggle
        label={t('create.health_disable')}
        checked={form.healthDisable}
        onChange={(v) => set('healthDisable', v)}
      />
    </div>
  );
}

export function AdvancedTab({ form, set, canPrivileged }: TabProps) {
  const { t } = useTranslation();

  return (
    <div className="space-y-6">
      <Section title={t('create.log_driver')} description={t('create.log_driver_hint')}>
        <input
          className="input font-mono"
          value={form.logDriver}
          placeholder="json-file"
          aria-label={t('create.log_driver')}
          onChange={(e) => set('logDriver', e.target.value)}
        />
        {form.logDriver.trim() !== '' && (
          <PairEditor
            rows={form.logOptions}
            onChange={(rows) => set('logOptions', rows)}
            addLabel={t('create.log_options')}
            emptyLabel={t('create.log_options')}
            keyPlaceholder="max-size"
          />
        )}
      </Section>

      <Section title={t('create.cap_drop')} description={t('create.cap_drop_hint')}>
        <input
          className="input font-mono"
          value={form.capDrop}
          placeholder="ALL"
          aria-label={t('create.cap_drop')}
          onChange={(e) => set('capDrop', e.target.value)}
        />
      </Section>

      <Section title={t('create.privileged_title')}>
        <PrivilegedNotice allowed={canPrivileged}>
          {canPrivileged ? t('create.privileged_allowed') : t('create.privileged_denied')}
        </PrivilegedNotice>

        <div className="space-y-4">
          <Toggle
            label={t('create.privileged')}
            hint={t('create.privileged_hint')}
            checked={form.privileged}
            onChange={(v) => set('privileged', v)}
          />
          <Toggle
            label={t('create.read_only_root')}
            checked={form.readOnlyRootFS}
            onChange={(v) => set('readOnlyRootFS', v)}
          />

          <Field label={t('create.cap_add')}>
            <input
              className="input font-mono"
              value={form.capAdd}
              placeholder="NET_ADMIN, SYS_TIME"
              aria-label={t('create.cap_add')}
              onChange={(e) => set('capAdd', e.target.value)}
            />
          </Field>

          <Field label={t('create.security_opt')}>
            <input
              className="input font-mono"
              value={form.securityOpt}
              placeholder="no-new-privileges:true"
              aria-label={t('create.security_opt')}
              onChange={(e) => set('securityOpt', e.target.value)}
            />
          </Field>

          <Field label={t('create.devices')} hint={t('create.devices_hint')}>
            <input
              className="input font-mono"
              value={form.devices}
              placeholder="/dev/ttyUSB0"
              aria-label={t('create.devices')}
              onChange={(e) => set('devices', e.target.value)}
            />
          </Field>

          <Field label={t('create.sysctls')}>
            <PairEditor
              rows={form.sysctls}
              onChange={(rows) => set('sysctls', rows)}
              addLabel={t('create.sysctls')}
              emptyLabel={t('create.sysctls')}
              keyPlaceholder="net.ipv4.ip_forward"
            />
          </Field>
        </div>
      </Section>
    </div>
  );
}
