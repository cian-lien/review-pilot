import type { RepoAccess, TenantRef } from '@reviewpilot/domain';
export interface Installation {
  id: number;
  appId: number;
  suspended: boolean;
}
export interface Repository {
  id: number;
  fullName: string;
  public: boolean;
}
export interface UserRepository extends Repository {
  permissions: { pull: boolean; maintain: boolean; admin: boolean };
}
export interface AccessGateway {
  userInstallations(): Promise<Installation[]>;
  installationRepositories(id: number): Promise<Repository[]>;
  userRepository(fullName: string): Promise<UserRepository>;
}
export async function requireRepositoryAccess(
  gateway: AccessGateway,
  tenant: TenantRef,
  action: 'view' | 'manage',
  appId: number,
): Promise<RepoAccess> {
  const installation = (await gateway.userInstallations()).find(
    (item) => item.id === tenant.installationId && item.appId === appId && !item.suspended,
  );
  if (!installation) throw new Error('Installation access denied');
  const repository = (await gateway.installationRepositories(installation.id)).find(
    (item) => item.id === tenant.repositoryId,
  );
  if (!repository?.public) throw new Error('Public repository access required');
  const userRepository = await gateway.userRepository(repository.fullName);
  if (
    userRepository.id !== tenant.repositoryId ||
    !userRepository.public ||
    !userRepository.permissions.pull
  )
    throw new Error('Repository access denied');
  const maintainer = userRepository.permissions.maintain || userRepository.permissions.admin;
  if (action === 'manage' && !maintainer) throw new Error('Maintainer permission required');
  return { tenant, public: true, installationActive: true, maintainer };
}
