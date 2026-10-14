module Feature.Search exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , RequestKind(..)
    , State(..)
    , context
    , errorToString
    , hasMore
    , init
    , isBusy
    , nextCursor
    , query
    , refreshPrefix
    , prefixPageLimit
    , requestId
    , requestMore
    , reset
    , restart
    , start
    , state
    , update
    )

{-| Ordinary search lifecycle: submission, loading state, pagination, and
stale-response rejection for the global post query.

The feature owns the request generation (`requestId`), the loading state,
the current query, and the next-page cursor. Every fresh submission bumps
the generation, so late completions from an earlier query are ignored.
Appends reuse the current generation so a newer submission invalidates an
in-flight append. Collection browsing lives in `Feature.Collections` with
its own generation; the page drops late search arrivals while a
collection is browsed.

Result integration stays with the caller: `update` reports arrivals as
`OutMsg` values and the page merges posts into its sequence, selection,
mode, and scroll position. Route pushes, pending-route tracking, and the
query draft also stay with the page.
-}

import Api.Error as ApiError
import Api.Post
import Domain.Post exposing (PostSummary, SearchResponse)
import Http
import Set exposing (Set)


{-| Page size for ordinary search requests.
-}
pageSize : Int
pageSize =
    60


type State
    = Idle
    | Searching
    | LoadingMore
    | RefreshingPrefix
    | Ready
    | Failed String


type RequestKind
    = InitialRequest
    | MoreRequest
    | PrefixRequest


type alias Model =
    { query : String
    , state : State
    , requestId : Int
    , nextCursor : Maybe String
    , prefix : Maybe PrefixRefresh
    }


type alias PrefixRefresh =
    { target : Int
    , fetched : List PostSummary
    , fetchedIds : Set String
    , seenCursors : Set String
    , pages : Int
    , returnCursor : Maybe String
    , returnState : State
    }


{-| HTTP context for search requests. Public search never carries a
credential.
-}
type alias Context =
    { apiBase : String
    }


context : String -> Context
context apiBase =
    { apiBase = apiBase
    }


type Msg
    = Completed Int RequestKind (Result Http.Error SearchResponse)


type OutMsg
    = InitialArrived SearchResponse
    | MoreArrived SearchResponse
    | PrefixArrived SearchResponse
    | SearchFailed Http.Error


init : Model
init =
    { query = ""
    , state = Idle
    , requestId = 0
    , nextCursor = Nothing
    , prefix = Nothing
    }


query : Model -> String
query model =
    model.query


state : Model -> State
state model =
    model.state


requestId : Model -> Int
requestId model =
    model.requestId


nextCursor : Model -> Maybe String
nextCursor model =
    model.nextCursor


{-| A search request is in flight (fresh or paginating). The old results
stay on screen while this is true.
-}
isBusy : Model -> Bool
isBusy model =
    case model.state of
        Searching ->
            True

        LoadingMore ->
            True

        RefreshingPrefix ->
            True

        _ ->
            False


{-| An ordinary search page is ready and another page is available. Scroll
geometry stays with the caller; combine this with the grid mode there.
-}
hasMore : Model -> Bool
hasMore model =
    case ( model.state, model.nextCursor ) of
        ( Ready, Just _ ) ->
            True

        _ ->
            False


{-| Clear back to the empty state, abandoning in-flight work.
-}
reset : Model -> Model
reset model =
    { query = ""
    , state = Idle
    , requestId = model.requestId + 1
    , nextCursor = Nothing
    , prefix = Nothing
    }


{-| Submit a fresh search, abandoning in-flight work.
-}
start : Context -> String -> Model -> ( Model, Cmd Msg )
start ctx newQuery model =
    let
        id =
            model.requestId + 1
    in
    ( { model | query = newQuery, state = Searching, requestId = id, nextCursor = Nothing, prefix = Nothing }
    , Api.Post.search ctx.apiBase newQuery Nothing pageSize (Completed id InitialRequest)
    )


{-| Re-run the current query as a fresh search (manual retry).
-}
restart : Context -> Model -> ( Model, Cmd Msg )
restart ctx model =
    start ctx model.query model


{-| Ask for the next page when the feature holds a cursor. Without a
cursor this is a no-op and the loading state is untouched. Appends reuse
the current generation so a newer submission invalidates them.
-}
requestMore : Context -> Model -> ( Model, Cmd Msg )
requestMore ctx model =
    case model.nextCursor of
        Just cursor ->
            ( { model | state = LoadingMore }
            , Api.Post.search ctx.apiBase model.query (Just cursor) pageSize (Completed model.requestId MoreRequest)
            )

        Nothing ->
            ( model, Cmd.none )


{-| Re-read the currently loaded prefix (bounded to 1000 posts) without
publishing anything until the whole prefix has arrived. Starting any fresh
search, reset, or new prefix refresh cancels this one by generation.
-}
refreshPrefix : Context -> Int -> Model -> ( Model, Cmd Msg )
refreshPrefix ctx loadedCount model =
    if loadedCount <= 0 || model.state == Idle then
        ( model, Cmd.none )

    else
        let
            id =
                model.requestId + 1

            target =
                min loadedCount 1000
        in
        ( { model
            | state = RefreshingPrefix
            , requestId = id
            , prefix =
                Just
                    { target = target
                    , fetched = []
                    , fetchedIds = Set.empty
                    , seenCursors = Set.empty
                    , pages = 0
                    , returnCursor = model.nextCursor
                    , returnState = Ready
                    }
          }
        , Api.Post.search ctx.apiBase model.query Nothing (prefixPageLimit target 0) (Completed id PrefixRequest)
        )


{-| Keep a refresh cursor aligned with every row published, even when the
loaded prefix ends between normal page boundaries.
-}
prefixPageLimit : Int -> Int -> Int
prefixPageLimit target fetchedCount =
    max 1 (min pageSize (target - fetchedCount))


maxPrefixPages : Int
maxPrefixPages =
    20


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        Completed id kind result ->
            if id /= model.requestId then
                -- Stale: a newer submission has taken over the generation.
                ( model, Cmd.none, [] )

            else
                case ( kind, result ) of
                    ( InitialRequest, Ok response ) ->
                        ( { model | state = Ready, nextCursor = response.nextCursor }
                        , Cmd.none
                        , [ InitialArrived response ]
                        )

                    ( MoreRequest, Ok response ) ->
                        ( { model | state = Ready, nextCursor = response.nextCursor }
                        , Cmd.none
                        , [ MoreArrived response ]
                        )

                    ( PrefixRequest, Ok response ) ->
                        prefixPage ctx response model

                    ( PrefixRequest, Err error ) ->
                        case model.prefix of
                            Just prefix ->
                                ( { model | state = prefix.returnState, nextCursor = prefix.returnCursor, prefix = Nothing }
                                , Cmd.none
                                , [ SearchFailed error ]
                                )

                            Nothing ->
                                ( { model | state = Failed (errorToString error) }
                                , Cmd.none
                                , [ SearchFailed error ]
                                )

                    ( _, Err error ) ->
                        ( { model | state = Failed (errorToString error) }
                        , Cmd.none
                        , [ SearchFailed error ]
                        )


prefixPage : Context -> SearchResponse -> Model -> ( Model, Cmd Msg, List OutMsg )
prefixPage ctx response model =
    case model.prefix of
        Nothing ->
            ( model, Cmd.none, [] )

        Just prefix ->
            let
                pageIds =
                    List.map .id response.posts

                duplicateIds =
                    List.length pageIds /= Set.size (Set.fromList pageIds)
                        || List.any (\id -> Set.member id prefix.fetchedIds) pageIds

                oversizedPage =
                    List.length response.posts > prefixPageLimit prefix.target (List.length prefix.fetched)

                fresh =
                    response.posts

                fetched =
                    prefix.fetched ++ fresh

                fetchedIds =
                    List.foldl (\post ids -> Set.insert post.id ids) prefix.fetchedIds fresh

                pages =
                    prefix.pages + 1

                repeatedCursor =
                    case response.nextCursor of
                        Just cursor ->
                            Set.member cursor prefix.seenCursors

                        Nothing ->
                            False

                abort =
                    ( { model | state = prefix.returnState, nextCursor = prefix.returnCursor, prefix = Nothing }
                    , Cmd.none
                    , [ SearchFailed (Http.BadBody "The library changed while refreshing. Retry the search.") ]
                    )
            in
            if duplicateIds || oversizedPage || repeatedCursor || pages > maxPrefixPages then
                abort

            else if List.length fetched >= prefix.target || response.nextCursor == Nothing then
                ( { model | state = Ready, nextCursor = response.nextCursor, prefix = Nothing }
                , Cmd.none
                , [ PrefixArrived { posts = fetched, nextCursor = response.nextCursor } ]
                )

            else
                case response.nextCursor of
                    Just cursor ->
                        ( { model
                            | prefix =
                                Just
                                    { prefix
                                        | fetched = fetched
                                        , fetchedIds = fetchedIds
                                        , pages = pages
                                        , seenCursors = Set.insert cursor prefix.seenCursors
                                    }
                          }
                        , Api.Post.search ctx.apiBase model.query (Just cursor) (prefixPageLimit prefix.target (List.length fetched)) (Completed model.requestId PrefixRequest)
                        , []
                        )

                    Nothing ->
                        abort


errorToString : Http.Error -> String
errorToString error =
    case ApiError.classify error of
        ApiError.BadUrl url ->
            "Invalid API URL: " ++ url

        ApiError.Timeout ->
            "The search timed out."

        ApiError.NetworkError ->
            "The search server could not be reached."

        ApiError.BadStatus status ->
            "Search failed (HTTP " ++ String.fromInt status ++ ")."

        ApiError.BadBody ->
            "The search response was invalid."
