import { discoverDrafts, inspectDirectDraft } from '@/lib/sleeper/discovery';
import { SleeperApiError } from '@/lib/sleeper/client';

export async function POST(request: Request) {
  try {
    const body = await request.json() as { username?: string; draftId?: string };
    const username = body.username?.trim();
    if (!username) return Response.json({ error: 'Enter your Sleeper username.' }, { status: 400 });
    const draftId = body.draftId?.match(/[0-9]{5,}/)?.[0];
    const result = draftId
      ? await inspectDirectDraft(draftId, username)
      : await discoverDrafts(username);
    return Response.json(result);
  } catch (error) {
    if (error instanceof SleeperApiError && error.status === 404) {
      return Response.json({ error: 'We could not find that Sleeper user or draft.' }, { status: 404 });
    }
    console.error('Draft discovery failed', error);
    return Response.json({ error: 'Sleeper is not responding right now. Try again shortly.' }, { status: 502 });
  }
}
