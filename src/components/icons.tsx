import type { SVGProps } from 'react'

// Hand-rolled icon set — 20x20, stroke = currentColor, 1.6 weight. Keeping
// this as plain SVG rather than pulling an icon package keeps the dependency
// list to what the product actually needs.
export type IconProps = SVGProps<SVGSVGElement>

const base: IconProps = { viewBox: '0 0 20 20', fill: 'none', stroke: 'currentColor', strokeWidth: 1.6, strokeLinecap: 'round', strokeLinejoin: 'round' }

export const WrenchIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M13.5 3.5a3.5 3.5 0 0 0-4.6 4.6L3 14v3h3l5.9-5.9a3.5 3.5 0 0 0 4.6-4.6l-2.3 2.3-1.6-1.6 2.3-2.3z" />
  </svg>
)

export const BuildingIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <rect x="4" y="2.5" width="9" height="15" rx="0.5" />
    <path d="M13 8h3v9.5H4" />
    <path d="M7 6h1.5M7 9h1.5M7 12h1.5M10.5 6H12M10.5 9H12M10.5 12H12" />
  </svg>
)

export const ClipboardIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <rect x="4.5" y="3.5" width="11" height="14" rx="1.2" />
    <path d="M7.5 3.5a2.5 2.5 0 0 1 5 0" />
    <path d="M7 9h6M7 12h6M7 15h3.5" />
  </svg>
)

export const ChartIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M3 17V3" />
    <path d="M3 17h14" />
    <rect x="6" y="10" width="2.4" height="7" />
    <rect x="10" y="6" width="2.4" height="11" />
    <rect x="14" y="12" width="2.4" height="5" />
  </svg>
)

export const GearIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <circle cx="10" cy="10" r="2.6" />
    <path d="M10 2.8v1.9M10 15.3v1.9M17.2 10h-1.9M4.7 10H2.8M15.1 4.9l-1.35 1.35M6.25 13.75 4.9 15.1M15.1 15.1l-1.35-1.35M6.25 6.25 4.9 4.9" />
  </svg>
)

export const PlusIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M10 4v12M4 10h12" />
  </svg>
)

export const ChevronLeftIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M12.5 4.5 6.5 10l6 5.5" />
  </svg>
)

export const ChevronRightIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M7.5 4.5 13.5 10l-6 5.5" />
  </svg>
)

export const MapPinIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M10 17.5S4 12 4 7.8a6 6 0 0 1 12 0C16 12 10 17.5 10 17.5Z" />
    <circle cx="10" cy="7.8" r="1.9" />
  </svg>
)

export const CameraIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M3 6.5h2.6L7 4.5h6l1.4 2H17v9.5H3z" />
    <circle cx="10" cy="11" r="2.8" />
  </svg>
)

export const LogoutIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M8 17H4.5A1.5 1.5 0 0 1 3 15.5v-11A1.5 1.5 0 0 1 4.5 3H8" />
    <path d="M13 14l4-4-4-4M17 10H7.5" />
  </svg>
)

export const SunIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <circle cx="10" cy="10" r="3.4" />
    <path d="M10 2.5v1.8M10 15.7v1.8M17.5 10h-1.8M4.3 10H2.5M15.1 4.9l-1.27 1.27M6.16 13.84l-1.27 1.27M15.1 15.1l-1.27-1.27M6.16 6.16 4.9 4.9" />
  </svg>
)

export const MoonIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M16.5 12.3A7 7 0 0 1 7.7 3.5a7 7 0 1 0 8.8 8.8Z" />
  </svg>
)

export const SearchIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <circle cx="8.8" cy="8.8" r="5.3" />
    <path d="M16.5 16.5l-3.6-3.6" />
  </svg>
)

export const XIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M5 5l10 10M15 5 5 15" />
  </svg>
)

export const CheckIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M4 10.5l4 4 8-9" />
  </svg>
)

export const AlertIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M10 3 2.5 16.5h15L10 3Z" />
    <path d="M10 8v4M10 14.5v.01" />
  </svg>
)

export const UsersIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <circle cx="7.2" cy="7" r="2.6" />
    <path d="M2.5 17c.5-3 2.3-4.5 4.7-4.5S11.9 14 12.4 17" />
    <circle cx="14" cy="7.6" r="2.1" />
    <path d="M14.2 12.6c2 .3 3.2 1.7 3.6 4.4" />
  </svg>
)

export const LinkIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M8.3 11.7 11.7 8.3" />
    <path d="M9.3 5.6l1-1a3 3 0 0 1 4.2 4.2l-1.4 1.4M10.7 14.4l-1 1a3 3 0 0 1-4.2-4.2l1.4-1.4" />
  </svg>
)

export const HomeIcon = (p: IconProps) => (
  <svg {...base} {...p}>
    <path d="M3.5 9.5 10 3.5l6.5 6" />
    <path d="M5 8.5V16h10V8.5" />
  </svg>
)
