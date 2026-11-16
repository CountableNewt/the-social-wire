import { handlePreparationRequest } from "@/lib/anypub/preparationRequest";

export function POST(request: Request): Promise<Response> {
  return handlePreparationRequest(request);
}
