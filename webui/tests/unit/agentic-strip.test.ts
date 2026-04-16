import { describe, it, expect } from 'vitest';
import { AGENTIC_REGEX } from '$lib/constants/agentic';
import { parseAgenticContent } from '$lib/utils/agentic';

// Mirror the logic in ChatService.stripReasoningContent so we can test it in isolation.
// The real function is private static, so we replicate the strip pipeline here.
function stripContextMarkers(content: string): string {
        return content
                .replace(AGENTIC_REGEX.REASONING_BLOCK, '')
                .replace(AGENTIC_REGEX.REASONING_OPEN, '')
                .replace(AGENTIC_REGEX.AGENTIC_TOOL_CALL_BLOCK, '')
                .replace(AGENTIC_REGEX.AGENTIC_TOOL_CALL_OPEN, '');
}

// A realistic complete tool call block as stored in message.content after a turn.
const COMPLETE_BLOCK =
        '\n\n<<<AGENTIC_TOOL_CALL_START>>>\n' +
        '<<<TOOL_NAME:bash_tool>>>\n' +
        '<<<TOOL_ARGS_START>>>\n' +
        '{"command":"ls /tmp","description":"list tmp"}\n' +
        '<<<TOOL_ARGS_END>>>\n' +
        'file1.txt\nfile2.txt\n' +
        '<<<AGENTIC_TOOL_CALL_END>>>\n';

// Partial block: streaming was cut before END arrived.
const OPEN_BLOCK =
        '\n\n<<<AGENTIC_TOOL_CALL_START>>>\n' +
        '<<<TOOL_NAME:bash_tool>>>\n' +
        '<<<TOOL_ARGS_START>>>\n' +
        '{"command":"ls /tmp","description":"list tmp"}\n' +
        '<<<TOOL_ARGS_END>>>\n' +
        'partial output...';

describe('agentic marker stripping for context', () => {
        it('strips a complete tool call block, leaving surrounding text', () => {
                const input = 'Before.' + COMPLETE_BLOCK + 'After.';
                const result = stripContextMarkers(input);
                // markers gone; residual newlines between fragments are fine
                expect(result).not.toContain('<<<');
                expect(result).toContain('Before.');
                expect(result).toContain('After.');
        });

        it('strips multiple complete tool call blocks', () => {
                const input = 'A' + COMPLETE_BLOCK + 'B' + COMPLETE_BLOCK + 'C';
                const result = stripContextMarkers(input);
                expect(result).not.toContain('<<<');
                expect(result).toContain('A');
                expect(result).toContain('B');
                expect(result).toContain('C');
        });

        it('strips an open/partial tool call block (no END marker)', () => {
                const input = 'Lead text.' + OPEN_BLOCK;
                const result = stripContextMarkers(input);
                expect(result).toBe('Lead text.');
                expect(result).not.toContain('<<<');
        });

        it('does not alter content with no markers', () => {
                const input = 'Just a normal assistant response.';
                expect(stripContextMarkers(input)).toBe(input);
        });

        it('strips reasoning block independently', () => {
                const input = '<<<reasoning_content_start>>>think hard<<<reasoning_content_end>>>Answer.';
                expect(stripContextMarkers(input)).toBe('Answer.');
        });

        it('strips both reasoning and agentic blocks together', () => {
                const input =
                        '<<<reasoning_content_start>>>plan<<<reasoning_content_end>>>' +
                        'Some text.' +
                        COMPLETE_BLOCK;
                expect(stripContextMarkers(input)).not.toContain('<<<');
                expect(stripContextMarkers(input)).toContain('Some text.');
        });

        it('empty string survives', () => {
                expect(stripContextMarkers('')).toBe('');
        });
});

describe('parseAgenticContent strips orphaned markers', () => {
        it('strips orphaned AGENTIC_TOOL_CALL_END from TEXT section', () => {
                // Model leaked an END marker without a corresponding START
                const input = 'Some text.\n<<<AGENTIC_TOOL_CALL_END>>>';
                const sections = parseAgenticContent(input);
                // Should have exactly one TEXT section with the marker removed
                expect(sections.length).toBe(1);
                expect(sections[0].type).toBe('text');
                expect(sections[0].content).toBe('Some text.');
                expect(sections[0].content).not.toContain('<<<');
        });

        it('strips orphaned AGENTIC_TOOL_CALL_START from TEXT section', () => {
                const input = 'Hello\n<<<AGENTIC_TOOL_CALL_START>>>\nWorld';
                const sections = parseAgenticContent(input);
                const textContents = sections
                        .filter(s => s.type === 'text')
                        .map(s => s.content);
                const combined = textContents.join('');
                expect(combined).not.toContain('<<<');
        });

        it('strips orphaned TOOL_NAME marker from TEXT section', () => {
                const input = 'Result:\n<<<TOOL_NAME:some_tool>>>\ndone';
                const sections = parseAgenticContent(input);
                const textContents = sections
                        .filter(s => s.type === 'text')
                        .map(s => s.content);
                const combined = textContents.join('');
                expect(combined).not.toContain('<<<');
        });

        it('removes TEXT section that becomes empty after stripping', () => {
                const input = '<<<AGENTIC_TOOL_CALL_END>>>';
                const sections = parseAgenticContent(input);
                const textSections = sections.filter(s => s.type === 'text');
                expect(textSections.length).toBe(0);
        });

        it('preserves properly structured tool call blocks', () => {
                const input =
                        'Text before.\n' +
                        '<<<AGENTIC_TOOL_CALL_START>>>\n' +
                        '<<<TOOL_NAME:smart_shell>>>\n' +
                        '<<<TOOL_ARGS_START>>>{"command":"ls"}<<<TOOL_ARGS_END>>>\n' +
                        'result output\n' +
                        '<<<AGENTIC_TOOL_CALL_END>>>\n' +
                        'Text after.';
                const sections = parseAgenticContent(input);
                // Should have: TEXT, TOOL_CALL, TEXT
                const textSections = sections.filter(s => s.type === 'text');
                const toolSections = sections.filter(s => s.type === 'tool_call');
                expect(toolSections.length).toBe(1);
                expect(toolSections[0].toolName).toBe('smart_shell');
                expect(textSections.length).toBe(2);
                expect(textSections[0].content).toContain('Text before');
                expect(textSections[1].content).toContain('Text after');
        });
});
