import { notFound } from 'next/navigation';
import { ReviewConsole } from '@/features/reviews/review-console';
export default async function ReviewPage({
  params,
}: {
  params: Promise<{ installationId: string; repositoryId: string; number: string }>;
}) {
  const p = await params;
  const values = [Number(p.installationId),Number(p.repositoryId),Number(p.number)] as const;
  if (values.some((v) => !Number.isSafeInteger(v) || v < 1)) notFound();
  return (
    <ReviewConsole
      tenant={{ installationId: values[0], repositoryId: values[1] }}
      number={values[2]}
    />
  );
}
