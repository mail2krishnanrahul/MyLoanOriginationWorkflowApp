import { useCaseListStore } from '@/stores/caseListStore';
import { useCaseList } from '@/hooks/useCaseList';
import { useDebouncedValue } from '@/hooks/useDebouncedValue';
import { CaseCard } from './CaseCard';
import { Loader2, Inbox } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

export function CaseCardGrid() {
    const store = useCaseListStore();
    const navigate = useNavigate();

    // Debounce search string only
    const debouncedSearch = useDebouncedValue(store.filters.search, 400);

    const filtersToUse = { ...store.filters, search: debouncedSearch };

    const { data, isLoading, isError, error, isFetching } = useCaseList(filtersToUse, store.page, store.pageSize);

    const handleCaseClick = (id: string) => {
        navigate(`/cases/${id}`);
    };

    const handleTagClick = (tagCode: string) => {
        if (!store.filters.skillCodes.includes(tagCode)) {
            store.setAdvancedFilter('skillCodes', [...store.filters.skillCodes, tagCode]);
        }
    };

    if (isLoading) {
        return (
            <div className="flex flex-col items-center justify-center py-20 text-gray-400">
                <Loader2 className="w-8 h-8 animate-spin mb-4 text-blue-500" />
                <p>Loading cases...</p>
            </div>
        );
    }

    if (isError) {
        return (
            <div className="bg-red-50 border border-red-200 rounded-xl p-8 text-center">
                <h3 className="text-lg font-semibold text-red-800 mb-2">Error loading cases</h3>
                <p className="text-red-600">{error?.message || 'Please try again later or contact support.'}</p>
                <button
                    onClick={() => window.location.reload()}
                    className="mt-4 px-4 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700 font-medium transition-colors"
                >
                    Refresh Page
                </button>
            </div>
        );
    }

    if (!data?.items?.length) {
        return (
            <div className="bg-gray-50 border border-gray-200 border-dashed rounded-xl p-16 text-center flex flex-col items-center">
                <div className="w-16 h-16 bg-gray-100 rounded-full flex items-center justify-center mb-4">
                    <Inbox className="w-8 h-8 text-gray-400" />
                </div>
                <h3 className="text-lg font-semibold text-gray-900 mb-1">No cases found</h3>
                <p className="text-gray-500 max-w-sm mb-6">We couldn't find any cases matching your current filters. Try adjusting your search criteria.</p>
                <button
                    onClick={store.resetFilters}
                    className="px-4 py-2 bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 font-medium transition-colors focus:ring-2 focus:ring-blue-500 outline-none"
                >
                    Clear All Filters
                </button>
            </div>
        );
    }

    const totalPages = Math.ceil(data.totalCount / store.pageSize);

    return (
        <div className="space-y-6 relative">
            {isFetching && !isLoading && (
                <div className="absolute top-0 left-0 w-full h-1 bg-transparent overflow-hidden rounded-t-xl z-50 -mt-2">
                    <div className="h-full bg-blue-500 animate-pulse w-1/3"></div>
                </div>
            )}

            <div className="flex items-center justify-between text-sm text-gray-500">
                <span>Showing {data.items.length} of {data.totalCount} cases</span>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-6">
                {data.items.map(item => (
                    <CaseCard
                        key={item.id}
                        caseItem={item}
                        onClick={handleCaseClick}
                        onTagClick={handleTagClick}
                    />
                ))}
            </div>

            {totalPages > 1 && (
                <div className="flex items-center justify-center py-8 gap-2 border-t border-gray-100">
                    <button
                        onClick={() => {
                            store.setPage(Math.max(1, store.page - 1));
                            window.scrollTo({ top: 0, behavior: 'smooth' });
                        }}
                        disabled={store.page === 1 || isFetching}
                        className="px-4 py-2 border rounded-lg text-sm font-medium disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 transition-colors focus:ring-2 focus:ring-blue-500 outline-none"
                    >
                        Previous
                    </button>
                    <span className="text-sm font-medium text-gray-600 px-4">
                        Page {store.page} of {totalPages}
                    </span>
                    <button
                        onClick={() => {
                            store.setPage(Math.min(totalPages, store.page + 1));
                            window.scrollTo({ top: 0, behavior: 'smooth' });
                        }}
                        disabled={store.page >= totalPages || isFetching || !data.hasNextPage}
                        className="px-4 py-2 border rounded-lg text-sm font-medium disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 transition-colors focus:ring-2 focus:ring-blue-500 outline-none"
                    >
                        Next
                    </button>
                </div>
            )}
        </div>
    );
}
