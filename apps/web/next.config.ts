import type { NextConfig } from 'next';
const config: NextConfig = {
  transpilePackages: [
    '@reviewpilot/contracts',
    '@reviewpilot/domain',
    '@reviewpilot/persistence',
    '@reviewpilot/github',
  ],
  poweredByHeader: false,
  logging: { incomingRequests: false },
};
export default config;
