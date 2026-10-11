import { notFound } from 'next/navigation';
import { PullConsole } from '@/features/reviews/review-console';
export default async function PullPage({
  params,
}: {
  params: Promise<{ installationId: string; repositoryId: string }>;
}) {
  const p = await params;
  const values = [Number(p.installationId),Number(p.repositoryId)] as const;
  if (values.some((v) => !Number.isSafeInteger(v) || v < 1)) notFound();
  return <PullConsole tenant={{ installationId: values[0], repositoryId: values[1] }} />;
}
