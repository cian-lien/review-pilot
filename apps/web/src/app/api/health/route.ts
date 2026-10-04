export function GET(): Response {
  return Response.json({ status: 'ok', service: 'review-pilot', phase: 'foundation' });
}
