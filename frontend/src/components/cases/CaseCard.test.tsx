
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { CaseCard } from './CaseCard';
import { TagCategory, type CaseListItem } from '@/types/cases';

const mockCase: CaseListItem = {
    id: 'case-123',
    reference: 'REF-001',
    title: 'Test Borrower Case',
    description: 'A test case description',
    priority: 'HIGH',
    status: 'IN_PROGRESS',
    complexity: 'STANDARD_1',
    requiredSkills: ['CREDIT_ANALYST'],
    tags: [
        { tagCode: 'DOC_MISSING', tagValue: null, category: TagCategory.DOCUMENT_ERROR }
    ],
    assignedUser: { userId: 'u1', displayName: 'John Doe', initials: 'JD' },
    slaDueAt: new Date(Date.now() + 1000000).toISOString(),
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    hasBlockingErrors: false,
    isVip: true,
    stageCode: 'REVIEW'
};

describe('CaseCard Component', () => {
    it('renders correctly with given case data', () => {
        const onClickMock = vi.fn();
        render(<CaseCard caseItem={mockCase} onClick={onClickMock} />);

        expect(screen.getByText('Test Borrower Case')).toBeInTheDocument();
        expect(screen.getByText('REF-001')).toBeInTheDocument();
        expect(screen.getByText('A test case description')).toBeInTheDocument();
    });

    it('triggers onClick when clicked', () => {
        const onClickMock = vi.fn();
        render(<CaseCard caseItem={mockCase} onClick={onClickMock} />);

        fireEvent.click(screen.getByRole('button'));
        expect(onClickMock).toHaveBeenCalledWith('case-123');
    });

    it('triggers onTagClick when a tag is clicked', () => {
        const onClickMock = vi.fn();
        const onTagClickMock = vi.fn();
        render(<CaseCard caseItem={mockCase} onClick={onClickMock} onTagClick={onTagClickMock} />);

        const tag = screen.getByText('DOC_MISSING');
        fireEvent.click(tag);

        expect(onTagClickMock).toHaveBeenCalledWith('DOC_MISSING');
        expect(onClickMock).not.toHaveBeenCalled();
    });
});
