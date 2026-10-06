import type { NextConfig } from 'next';
const config: NextConfig = { transpilePackages: ['@reviewpilot/contracts'], poweredByHeader:false, logging:{incomingRequests:false} };
export default config;
