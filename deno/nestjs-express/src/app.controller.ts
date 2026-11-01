import { Controller, Get, Header } from '@nestjs/common';

@Controller()
export class AppController {
  @Get('text')
  @Header('Content-Type', 'text/plain')
  text(): string {
    return 'hello world';
  }

  @Get('json')
  json(): { message: string } {
    return { message: 'hello world' };
  }
}
