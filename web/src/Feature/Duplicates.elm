module Feature.Duplicates exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , context
    , currentHash
    , groups
    , init
    , isOpen
    , locked
    , members
    , membersLoading
    , groupsLoading
    , groupsRequestId
    , membersCount
    , membersRequestId
    , selectedIds
    , status
    , trashableIds
    , update
    , view
    )

{-| Bounded exact-copy review.

Groups page by hash (20 at a time) and one group's members page by id
(60 at a time); nothing here ever loads the whole library. The review
selection is this module's own set, independent of the Library grid
selection, capped at 100 ids, and never pre-filled: only an explicit
checkbox sends ids toward Trash, through the existing Trash
confirmation. Reads are public and work while locked; only the Trash
request is gated on write access.

Every response carries the access generation, a per-stream request id,
and (for members/metadata) its hash. Closing the panel, switching
groups, or a lock bumps the relevant ids, so late responses for other
groups, pages, or access epochs are dropped. Errors keep the current
safe state and wait for an explicit reload; mutations are never
retried here.
-}

import Api.Duplicates
import Api.Metadata
import Api.Post
import Dict exposing (Dict)
import Domain.Duplicates exposing (DuplicateGroup, DuplicateGroupsPage, DuplicateMembersPage)
import Domain.Post exposing (MetadataSnapshot, PostSummary)
import Html exposing (Html, button, div, input, label, li, p, span, text, ul)
import Html.Attributes exposing (attribute, checked, class, disabled, title, type_)
import Html.Events exposing (onCheck, onClick)
import Http
import Set exposing (Set)
import Ui.Media


groupsLimit : Int
groupsLimit =
    20


membersLimit : Int
membersLimit =
    60


maxSelected : Int
maxSelected =
    100


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    }


context : String -> Maybe String -> Bool -> Int -> Context
context apiBase credential writesEnabled accessGeneration =
    { apiBase = apiBase, credential = credential, writesEnabled = writesEnabled, accessGeneration = accessGeneration }


type alias Model =
    { open : Bool
    , groups : List DuplicateGroup
    , groupsNext : Maybe String
    , groupsLoading : Bool
    , groupsRequestId : Int
    , currentHash : Maybe String
    , members : List PostSummary
    , membersCount : Int
    , membersNext : Maybe String
    , membersLoading : Bool
    , membersRequestId : Int
    , selected : Set String
    , metadata : Dict String MetadataSnapshot
    , metadataRequestId : Int
    , missing : Set String
    , status : Maybe String
    }


init : Model
init =
    { open = False
    , groups = []
    , groupsNext = Nothing
    , groupsLoading = False
    , groupsRequestId = 0
    , currentHash = Nothing
    , members = []
    , membersCount = 0
    , membersNext = Nothing
    , membersLoading = False
    , membersRequestId = 0
    , selected = Set.empty
    , metadata = Dict.empty
    , metadataRequestId = 0
    , missing = Set.empty
    , status = Nothing
    }


isOpen : Model -> Bool
isOpen model =
    model.open


groups : Model -> List DuplicateGroup
groups model =
    model.groups


members : Model -> List PostSummary
members model =
    model.members


currentHash : Model -> Maybe String
currentHash model =
    model.currentHash


status : Model -> Maybe String
status model =
    model.status


groupsLoading : Model -> Bool
groupsLoading model =
    model.groupsLoading


groupsRequestId : Model -> Int
groupsRequestId model =
    model.groupsRequestId


membersRequestId : Model -> Int
membersRequestId model =
    model.membersRequestId


membersCount : Model -> Int
membersCount model =
    model.membersCount


membersLoading : Model -> Bool
membersLoading model =
    model.membersLoading


{-| Chosen ids in loaded-member (id ASC) order.
-}
selectedIds : Model -> List String
selectedIds model =
    model.members |> List.map .id |> List.filter (\id -> Set.member id model.selected)


{-| Ids that may be sent to Trash right now: explicitly chosen, still
loaded in the current group, and within the 100-id bound.
-}
trashableIds : Model -> List String
trashableIds model =
    selectedIds model |> List.take maxSelected


{-| Lock/revoke clears the pending write selection only. Public review
state stays readable; in-flight member/metadata responses go stale
through bumped request ids, and the Library clears any Trash
confirmation separately through `Trash.locked`.
-}
locked : Model -> Model
locked model =
    { model
        | selected = Set.empty
        , membersRequestId = model.membersRequestId + 1
        , metadataRequestId = model.metadataRequestId + 1
        , membersLoading = False
    }


type Msg
    = OpenRequested
    | CloseRequested
    | BackRequested
    | GroupsLoaded Int Int Bool (Result Http.Error DuplicateGroupsPage)
    | GroupsLoadMoreRequested
    | GroupsReloadRequested
    | GroupOpenRequested String
    | MembersLoaded Int Int String Bool (Result Http.Error DuplicateMembersPage)
    | MembersLoadMoreRequested
    | MembersReloadRequested
    | MetadataLoaded Int Int String (Result Http.Error (List MetadataSnapshot))
    | MemberToggled String Bool
    | SelectionCleared
    | CompareRequested
    | TrashRequested
    | MediaFailed String
    | TrashApplied (List String)
    | RestoreApplied (List String)


type OutMsg
    = CompareCopies String String (List PostSummary)
    | TrashSelected (List String)


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        OpenRequested ->
            if model.open then
                ( model, Cmd.none, [] )

            else if model.groupsLoading then
                ( { model | open = True }, Cmd.none, [] )

            else
                let
                    id =
                        model.groupsRequestId + 1
                in
                ( { model | open = True, groupsLoading = True, groupsRequestId = id, status = Just "Loading exact copies…" }
                , Api.Duplicates.listGroups ctx.apiBase Nothing groupsLimit (GroupsLoaded ctx.accessGeneration id False)
                , []
                )

        CloseRequested ->
            ( { model
                | open = False
                , currentHash = Nothing
                , members = []
                , membersCount = 0
                , membersNext = Nothing
                , selected = Set.empty
                , metadata = Dict.empty
                , groupsLoading = False
                , membersLoading = False
                , groupsRequestId = model.groupsRequestId + 1
                , membersRequestId = model.membersRequestId + 1
                , metadataRequestId = model.metadataRequestId + 1
              }
            , Cmd.none
            , []
            )

        BackRequested ->
            ( { model
                | currentHash = Nothing
                , members = []
                , membersCount = 0
                , membersNext = Nothing
                , membersLoading = False
                , selected = Set.empty
                , membersRequestId = model.membersRequestId + 1
                , metadataRequestId = model.metadataRequestId + 1
                , status = Nothing
              }
            , Cmd.none
            , []
            )

        GroupsLoadMoreRequested ->
            case ( model.open, model.groupsLoading, model.groupsNext ) of
                ( True, False, Just cursor ) ->
                    let
                        id =
                            model.groupsRequestId + 1
                    in
                    ( { model | groupsLoading = True, groupsRequestId = id, status = Just "Loading exact copies…" }
                    , Api.Duplicates.listGroups ctx.apiBase (Just cursor) groupsLimit (GroupsLoaded ctx.accessGeneration id True)
                    , []
                    )

                _ ->
                    ( model, Cmd.none, [] )

        GroupsReloadRequested ->
            if model.groupsLoading then
                ( model, Cmd.none, [] )

            else
                let
                    id =
                        model.groupsRequestId + 1
                in
                ( { model | open = True, groupsLoading = True, groupsRequestId = id, status = Just "Loading exact copies…" }
                , Api.Duplicates.listGroups ctx.apiBase Nothing groupsLimit (GroupsLoaded ctx.accessGeneration id False)
                , []
                )

        GroupsLoaded generation id appending result ->
            if generation /= ctx.accessGeneration || id /= model.groupsRequestId || not model.open then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok page ->
                        let
                            merged =
                                if appending then
                                    mergeGroups model.groups page.groups

                                else
                                    page.groups
                        in
                        ( { model | groups = merged, groupsNext = page.nextCursor, groupsLoading = False, status = Nothing }, Cmd.none, [] )

                    Err error ->
                        ( { model | groupsLoading = False, status = Just ("Exact copies could not be loaded (HTTP " ++ httpStatus error ++ "). Use Reload to try again.") }, Cmd.none, [] )

        GroupOpenRequested hash ->
            if model.membersLoading || not (Domain.Duplicates.isCanonicalHash hash) then
                ( model, Cmd.none, [] )

            else
                let
                    id =
                        model.membersRequestId + 1
                in
                ( { model
                    | currentHash = Just hash
                    , metadata = Dict.empty
                    , members = []
                    , membersCount = 0
                    , membersNext = Nothing
                    , membersLoading = True
                    , membersRequestId = id
                    , metadataRequestId = model.metadataRequestId + 1
                    , selected = Set.empty
                    , status = Just "Loading copies…"
                  }
                , Api.Duplicates.listMembers ctx.apiBase hash Nothing membersLimit (MembersLoaded ctx.accessGeneration id hash False)
                , []
                )

        MembersLoadMoreRequested ->
            case ( model.currentHash, model.membersLoading, model.membersNext ) of
                ( Just hash, False, Just cursor ) ->
                    let
                        id =
                            model.membersRequestId + 1
                    in
                    ( { model | membersLoading = True, membersRequestId = id, status = Just "Loading copies…" }
                    , Api.Duplicates.listMembers ctx.apiBase hash (Just cursor) membersLimit (MembersLoaded ctx.accessGeneration id hash True)
                    , []
                    )

                _ ->
                    ( model, Cmd.none, [] )

        MembersReloadRequested ->
            case model.currentHash of
                Just hash ->
                    if model.membersLoading then
                        ( model, Cmd.none, [] )

                    else
                        let
                            id =
                                model.membersRequestId + 1
                        in
                        ( { model | membersLoading = True, membersRequestId = id, status = Just "Loading copies…" }
                        , Api.Duplicates.listMembers ctx.apiBase hash Nothing membersLimit (MembersLoaded ctx.accessGeneration id hash False)
                        , []
                        )

                Nothing ->
                    ( model, Cmd.none, [] )

        MembersLoaded generation id hash appending result ->
            if generation /= ctx.accessGeneration
                || id /= model.membersRequestId
                || not model.open
                || model.currentHash /= Just hash
            then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok page ->
                        if page.hash /= hash || page.count < 2 then
                            ( { model | membersLoading = False, status = Just "That exact-copy group returned an unexpected response. Use Reload to try again." }, Cmd.none, [] )

                        else
                            let
                                merged =
                                    if appending then
                                        mergeMembers model.members page.posts

                                    else
                                        page.posts

                                pruned =
                                    Set.filter (\postId -> List.member postId (List.map .id merged)) model.selected

                                withMembers =
                                    { model
                                        | members = merged
                                        , membersCount = page.count
                                        , membersNext = page.nextCursor
                                        , membersLoading = False
                                        , selected = pruned
                                        , status = Nothing
                                    }

                                freshIds =
                                    if ctx.writesEnabled then
                                        List.map .id page.posts |> List.filter (\postId -> not (Dict.member postId model.metadata))

                                    else
                                        []
                            in
                            case freshIds of
                                [] ->
                                    ( withMembers, Cmd.none, [] )

                                _ ->
                                    let
                                        mid =
                                            model.metadataRequestId + 1
                                    in
                                    ( { withMembers | metadataRequestId = mid }
                                    , Api.Metadata.read ctx.apiBase ctx.credential freshIds
                                        (\metaResult ->
                                            MetadataLoaded ctx.accessGeneration mid hash (Result.map .posts metaResult)
                                        )
                                    , []
                                    )

                    Err (Http.BadStatus 404) ->
                        let
                            id2 =
                                model.groupsRequestId + 1
                        in
                        ( { model
                            | currentHash = Nothing
                            , members = []
                            , membersCount = 0
                            , membersNext = Nothing
                            , membersLoading = False
                            , selected = Set.empty
                            , groupsLoading = True
                            , groupsRequestId = id2
                            , status = Just "That exact-copy group is no longer available (it may have been trashed or hidden). The group list was reloaded; your Library view is unchanged."
                          }
                        , Api.Duplicates.listGroups ctx.apiBase Nothing groupsLimit (GroupsLoaded ctx.accessGeneration id2 False)
                        , []
                        )

                    Err error ->
                        ( { model | membersLoading = False, status = Just ("Copies could not be loaded (HTTP " ++ httpStatus error ++ "). Use Reload to try again.") }, Cmd.none, [] )

        MetadataLoaded generation id hash result ->
            if generation /= ctx.accessGeneration || id /= model.metadataRequestId || model.currentHash /= Just hash then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok snapshots ->
                        ( { model | metadata = List.foldl (\snap dict -> Dict.insert snap.id snap dict) model.metadata snapshots }, Cmd.none, [] )

                    Err _ ->
                        ( model, Cmd.none, [] )

        MemberToggled postId isChecked ->
            if model.membersLoading || not model.open || not (List.member postId (List.map .id model.members)) then
                ( model, Cmd.none, [] )

            else if isChecked then
                if Set.member postId model.selected then
                    ( model, Cmd.none, [] )

                else if Set.size model.selected >= maxSelected then
                    ( { model | status = Just "At most 100 copies can be chosen at once. Move some to Trash or clear the choice first." }, Cmd.none, [] )

                else
                    ( { model | selected = Set.insert postId model.selected, status = Nothing }, Cmd.none, [] )

            else
                ( { model | selected = Set.remove postId model.selected }, Cmd.none, [] )

        SelectionCleared ->
            ( { model | selected = Set.empty }, Cmd.none, [] )

        CompareRequested ->
            case model.members of
                first :: second :: _ ->
                    let
                        chosen =
                            selectedIds model

                        ( select, candidate ) =
                            case chosen of
                                a :: b :: _ ->
                                    ( a, b )

                                _ ->
                                    ( first.id, second.id )
                    in
                    case ( findMember select model.members, findMember candidate model.members ) of
                        ( Just _, Just _ ) ->
                            ( model, Cmd.none, [ CompareCopies select candidate model.members ] )

                        _ ->
                            ( { model | status = Just "Load at least two copies before comparing." }, Cmd.none, [] )

                _ ->
                    ( { model | status = Just "Load at least two copies before comparing." }, Cmd.none, [] )

        TrashRequested ->
            if model.membersLoading || model.groupsLoading then
                ( model, Cmd.none, [] )

            else if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to move posts to Trash." }, Cmd.none, [] )

            else
                case trashableIds model of
                    [] ->
                        ( { model | status = Just "Choose at least one copy with its checkbox before moving copies to Trash. Nothing is chosen automatically." }, Cmd.none, [] )

                    ids ->
                        ( model, Cmd.none, [ TrashSelected ids ] )

        MediaFailed postId ->
            ( { model | missing = Set.insert postId model.missing }, Cmd.none, [] )

        TrashApplied ids ->
            reconcileRemoved ids ctx model

        RestoreApplied _ ->
            reloadAfterRestore ctx model


reconcileRemoved : List String -> Context -> Model -> ( Model, Cmd Msg, List OutMsg )
reconcileRemoved ids ctx model =
    let
        removed =
            List.filter (\post -> List.member post.id ids) model.members

        pruned =
            { model
                | members = List.filter (\post -> not (List.member post.id ids)) model.members
                , membersCount = max 0 (model.membersCount - List.length removed)
                , selected = Set.filter (\postId -> not (List.member postId ids)) model.selected
                , metadataRequestId = model.metadataRequestId + 1
            }

        groupGone =
            model.currentHash /= Nothing && pruned.membersCount < 2
    in
    if groupGone then
        reloadAfterRestore ctx
            { pruned | currentHash = Nothing, members = [], membersNext = Nothing, selected = Set.empty }

    else
        reloadAfterRestore ctx pruned


reloadAfterRestore : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
reloadAfterRestore ctx model =
    if not model.open then
        ( { model | membersRequestId = model.membersRequestId + 1, groupsRequestId = model.groupsRequestId + 1, metadataRequestId = model.metadataRequestId + 1, membersLoading = False, groupsLoading = False }, Cmd.none, [] )

    else
        reloadOpenAfterRestore ctx model


reloadOpenAfterRestore : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
reloadOpenAfterRestore ctx model =
    -- Restored posts may rejoin a group, so re-read only the bounded
    -- group list and, when one is open, its first member page.
    let
        gid =
            model.groupsRequestId + 1

        base =
            { model | groupsLoading = True, groupsRequestId = gid, groups = model.groups }
    in
    case model.currentHash of
        Just hash ->
            let
                mid =
                    model.membersRequestId + 1
            in
            ( { base | membersLoading = True, membersRequestId = mid }
            , Cmd.batch
                [ Api.Duplicates.listGroups ctx.apiBase Nothing groupsLimit (GroupsLoaded ctx.accessGeneration gid False)
                , Api.Duplicates.listMembers ctx.apiBase hash Nothing membersLimit (MembersLoaded ctx.accessGeneration mid hash False)
                ]
            , []
            )

        Nothing ->
            ( base
            , Api.Duplicates.listGroups ctx.apiBase Nothing groupsLimit (GroupsLoaded ctx.accessGeneration gid False)
            , []
            )


mergeGroups : List DuplicateGroup -> List DuplicateGroup -> List DuplicateGroup
mergeGroups existing incoming =
    existing ++ List.filter (\group -> not (List.member group.hash (List.map .hash existing))) incoming


mergeMembers : List PostSummary -> List PostSummary -> List PostSummary
mergeMembers existing incoming =
    existing ++ List.filter (\post -> not (List.member post.id (List.map .id existing))) incoming


findMember : String -> List PostSummary -> Maybe PostSummary
findMember postId posts =
    List.filter (\post -> post.id == postId) posts |> List.head


httpStatus : Http.Error -> String
httpStatus error =
    case error of
        Http.BadStatus code ->
            String.fromInt code

        _ ->
            "network"


shortHash : String -> String
shortHash hash =
    String.left 20 hash ++ "…"


type alias ViewConfig msg =
    { apiBase : String
    , writesEnabled : Bool
    , onClose : msg
    , onBack : msg
    , onReloadGroups : msg
    , onLoadMoreGroups : msg
    , onOpenGroup : String -> msg
    , onReloadMembers : msg
    , onLoadMoreMembers : msg
    , onToggle : String -> Bool -> msg
    , onClearSelection : msg
    , onCompare : msg
    , onTrash : msg
    , onMediaError : String -> msg
    }


view : Model -> ViewConfig msg -> Html msg
view model config =
    div [ class "panel-section duplicates-view" ]
        ([ p [ class "panel-note" ] [ text "Exact copies share identical original bytes. This is not visual similarity, and nothing is chosen or deleted automatically." ]
         , case model.status of
            Just message ->
                p [ class "panel-note", attribute "role" "status" ] [ text message ]

            Nothing ->
                text ""
         ]
            ++ (case model.currentHash of
                    Just hash ->
                        memberView model config hash

                    Nothing ->
                        groupView model config
               )
        )


groupView : Model -> ViewConfig msg -> List (Html msg)
groupView model config =
    [ if List.isEmpty model.groups && not model.groupsLoading then
        p [ class "panel-note" ] [ text "No exact-copy groups found." ]

      else
        text ""
    , ul [ class "nav-list duplicates-groups" ]
        (List.map (groupRow config) model.groups)
    , p []
        [ button [ class "button button-quiet", type_ "button", onClick config.onLoadMoreGroups, disabled (model.groupsLoading || model.groupsNext == Nothing) ] [ text "Load more groups" ]
        , button [ class "button button-quiet", type_ "button", onClick config.onReloadGroups, disabled model.groupsLoading ] [ text "Reload" ]
        , button [ class "button button-quiet", type_ "button", onClick config.onClose ] [ text "Close" ]
        ]
    ]


groupRow : ViewConfig msg -> DuplicateGroup -> Html msg
groupRow config group =
    li [ class "nav-row duplicates-group-row" ]
        [ span [ class "duplicates-samples" ]
            (List.map
                (\post ->
                    Ui.Media.image [ class "duplicates-thumb" ]
                        { url = Api.Post.mediaUrl config.apiBase post.previewUrl
                        , postId = post.id
                        , missing = Domain.Post.isVideoMedia post.mediaType && Domain.Post.posterUrl post == Nothing
                        , onError = config.onMediaError
                        }
                )
                group.posts
            )
        , span [ class "duplicates-group-meta" ]
            [ span [] [ text (String.fromInt group.count ++ " copies") ]
            , span [ class "panel-note", title group.hash ] [ text (shortHash group.hash) ]
            ]
        , button [ class "button button-quiet", type_ "button", onClick (config.onOpenGroup group.hash) ] [ text "Review copies" ]
        ]


memberView : Model -> ViewConfig msg -> String -> List (Html msg)
memberView model config hash =
    let
        chosen =
            selectedIds model

        chosenCount =
            List.length chosen

        compareLabel =
            case chosen of
                a :: b :: _ ->
                    "Compare copies #" ++ a ++ " and #" ++ b

                _ ->
                    case model.members of
                        first :: second :: _ ->
                            "Compare first two copies (#" ++ first.id ++ " and #" ++ second.id ++ ")"

                        _ ->
                            "Compare copies"

        trashDisabled =
            not config.writesEnabled || chosenCount == 0 || model.membersLoading || model.groupsLoading
    in
    [ p [ class "panel-note", title hash ] [ text ("Group " ++ shortHash hash ++ " · " ++ String.fromInt model.membersCount ++ " visible copies · " ++ String.fromInt (List.length model.members) ++ " loaded") ]
    , ul [ class "nav-list duplicates-members" ]
        (List.map (memberRow model config) model.members)
    , if List.isEmpty model.members && not model.membersLoading then
        p [ class "panel-note" ] [ text "No copies loaded." ]

      else
        text ""
    , p []
        [ button [ class "button button-quiet", type_ "button", onClick config.onLoadMoreMembers, disabled (model.membersLoading || model.membersNext == Nothing) ] [ text "Load more copies" ]
        , button [ class "button button-quiet", type_ "button", onClick config.onReloadMembers, disabled model.membersLoading ] [ text "Reload copies" ]
        , button [ class "button button-quiet", type_ "button", onClick config.onBack ] [ text "All groups" ]
        , button [ class "button button-quiet", type_ "button", onClick config.onClose ] [ text "Close" ]
        ]
    , p []
        [ button [ class "button", type_ "button", onClick config.onCompare, disabled (List.length model.members < 2 || model.membersLoading) ] [ text compareLabel ]
        , span [ class "panel-note" ] [ text " Compare opens the regular Compare view; Back to grid returns here." ]
        ]
    , p []
        [ button [ class "button", type_ "button", onClick config.onTrash, disabled trashDisabled ] [ text ("Move chosen copies to Trash (" ++ String.fromInt chosenCount ++ "/100)") ]
        , button [ class "button button-quiet", type_ "button", onClick config.onClearSelection, disabled (chosenCount == 0) ] [ text "Clear choice" ]
        , span [ class "panel-note" ]
            [ text
                (if not config.writesEnabled then
                    " Unlock to move copies to Trash. Browsing copies stays available while locked."

                 else if chosenCount == 0 then
                    " Choose copies with their checkboxes first; nothing is chosen automatically."

                 else
                    " You will be asked to confirm before anything moves to Trash."
                )
            ]
        ]
    ]


memberRow : Model -> ViewConfig msg -> PostSummary -> Html msg
memberRow model config post =
    let
        snapshot =
            Dict.get post.id model.metadata

        source =
            snapshot |> Maybe.map .source |> Maybe.withDefault ""

        artist =
            snapshot |> Maybe.map .artist |> Maybe.withDefault ""

        describe label value =
            if String.trim value == "" then
                label ++ " —"

            else
                label ++ " " ++ value
    in
    li [ class "nav-row duplicates-member-row" ]
        [ label [ class "duplicates-member-check" ]
            [ input
                [ type_ "checkbox"
                , checked (Set.member post.id model.selected)
                , onCheck (config.onToggle post.id)
                , attribute "aria-label" ("Choose copy " ++ post.id ++ " for Trash")
                ]
                []
            ]
        , Ui.Media.image [ class "duplicates-thumb" ]
            { url = Api.Post.mediaUrl config.apiBase post.previewUrl
            , postId = post.id
            , missing = Set.member post.id model.missing || (Domain.Post.isVideoMedia post.mediaType && Domain.Post.posterUrl post == Nothing)
            , onError = config.onMediaError
            }
        , span [ class "duplicates-member-meta" ]
            [ span [] [ text ("#" ++ post.id ++ " · " ++ post.mediaType ++ " · " ++ String.fromInt post.width ++ "×" ++ String.fromInt post.height) ]
            , span [ class "panel-note" ] [ text (describe "Source:" source ++ " · " ++ describe "Artist:" artist) ]
            ]
        ]
