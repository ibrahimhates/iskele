import {
  Activity,
  Box,
  Cloud,
  Database,
  GitBranch,
  Globe,
  HardDrive,
  Inbox,
  KeyRound,
  Lock,
  Radio,
  RefreshCw,
  Route,
  ShieldCheck,
  Table,
  Workflow,
} from 'lucide-react';

/**
 * The icons a template may name.
 *
 * A fixed map rather than a dynamic import: templates name a lucide icon so
 * the catalog needs no image assets and works with no network, and pulling an
 * arbitrary named export at runtime would defeat tree-shaking and let a custom
 * template point at anything.
 */
const ICONS = {
  Activity,
  Box,
  Cloud,
  Database,
  GitBranch,
  Globe,
  HardDrive,
  Inbox,
  KeyRound,
  Lock,
  Radio,
  RefreshCw,
  Route,
  ShieldCheck,
  Table,
  Workflow,
} as const;

interface Props {
  name?: string;
  className?: string;
  size?: number;
}

/** Renders a template's icon, falling back to a generic box. */
export function TemplateIcon({ name, className, size = 20 }: Props) {
  const Icon = (name && ICONS[name as keyof typeof ICONS]) || Box;
  return <Icon size={size} className={className} aria-hidden />;
}
