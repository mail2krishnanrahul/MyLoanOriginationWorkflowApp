import { describe, it, expect } from 'vitest';
import { deriveTagCategory, TagCategory } from './cases';

describe('deriveTagCategory', () => {
    it('detects COMPLEXITY', () => {
        expect(deriveTagCategory('STANDARD_2')).toBe(TagCategory.COMPLEXITY);
        expect(deriveTagCategory('NON_STANDARD')).toBe(TagCategory.COMPLEXITY);
    });

    it('detects DOCUMENT_ERROR', () => {
        expect(deriveTagCategory('DOC_MISSING')).toBe(TagCategory.DOCUMENT_ERROR);
        expect(deriveTagCategory('ERROR_VALUE')).toBe(TagCategory.DOCUMENT_ERROR);
    });

    it('detects VIP_PRIORITY', () => {
        expect(deriveTagCategory('VIP')).toBe(TagCategory.VIP_PRIORITY);
        expect(deriveTagCategory('URGENT')).toBe(TagCategory.VIP_PRIORITY);
    });

    it('detects EXCEPTION', () => {
        expect(deriveTagCategory('EXCEPTION')).toBe(TagCategory.EXCEPTION);
        expect(deriveTagCategory('PANEL_REVIEW')).toBe(TagCategory.EXCEPTION);
    });

    it('detects SKILL', () => {
        expect(deriveTagCategory('CREDIT_ANALYST')).toBe(TagCategory.SKILL);
        expect(deriveTagCategory('SKILL_REVIEW')).toBe(TagCategory.SKILL);
    });

    it('defaults to DEFAULT for unknown', () => {
        expect(deriveTagCategory('SOME_RANDOM_TAG')).toBe(TagCategory.DEFAULT);
    });
});
